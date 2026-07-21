// processor.go — the fieldcrypto processor. Traverses pdata, applies encryption,
// whole-value masking, and in-field pattern masking to string-typed fields.
package fieldcryptoprocessor

import (
	"context"
	"regexp"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"
)

// keyIDAttr is the record attribute recording which key encrypted a field. This is the
// decryption contract: the decryptor reads this to pick the right key.
const keyIDAttr = "encryption.key_id"

// compiledPattern is a pattern rule with its regex compiled once at Start.
type compiledPattern struct {
	field string
	typ   string
	re    *regexp.Regexp
}

// fieldCrypto holds the shared, signal-independent processing logic and state.
type fieldCrypto struct {
	cfg    *Config
	logger *zap.Logger

	kp KeyProvider

	maskValue     string
	maskFields    map[string]struct{}
	maskTemplates map[string]string
	encryptFields map[string]struct{}
	// patternsByField maps a target field (attribute name or "body") to its rules.
	patternsByField map[string][]compiledPattern
}

func newFieldCrypto(cfg *Config, logger *zap.Logger) *fieldCrypto {
	fc := &fieldCrypto{
		cfg:             cfg,
		logger:          logger,
		maskValue:       cfg.MaskValue,
		maskFields:      make(map[string]struct{}, len(cfg.Mask.Fields)),
		maskTemplates:   make(map[string]string, len(cfg.Mask.FieldPatterns)),
		encryptFields:   make(map[string]struct{}, len(cfg.Encrypt.Fields)),
		patternsByField: make(map[string][]compiledPattern),
	}
	for _, f := range cfg.Mask.Fields {
		fc.maskFields[f] = struct{}{}
	}
	for _, p := range cfg.Mask.FieldPatterns {
		fc.maskTemplates[p.Field] = p.Pattern
	}
	for _, f := range cfg.Encrypt.Fields {
		fc.encryptFields[f] = struct{}{}
	}
	return fc
}

// start builds the KeyProvider and compiles pattern regexes. Called from each signal
// processor's Start so key material is only touched once the pipeline is running.
func (fc *fieldCrypto) start(ctx context.Context) error {
	kp, err := buildKeyProvider(fc.cfg)
	if err != nil {
		return err
	}
	fc.kp = kp

	for _, p := range fc.cfg.Mask.Patterns {
		cp := compiledPattern{field: p.Field, typ: p.Type}
		switch p.Type {
		case patternTypeCPF:
			cp.re = cpfCandidateRegex
		case patternTypeCNPJ:
			cp.re = cnpjCandidateRegex
		case patternTypeIBAN:
			cp.re = ibanCandidateRegex
		case patternTypeRegex:
			cp.re = regexp.MustCompile(p.Regex) // already validated in Config.Validate
		}
		fc.patternsByField[p.Field] = append(fc.patternsByField[p.Field], cp)
	}
	fc.logger.Debug("fieldcrypto started",
		zap.String("key_provider", fc.cfg.KeyProvider),
		zap.Int("mask_fields", len(fc.maskFields)),
		zap.Int("mask_template_fields", len(fc.maskTemplates)),
		zap.Int("encrypt_fields", len(fc.encryptFields)),
		zap.Int("pattern_fields", len(fc.patternsByField)))
	return nil
}

// applyPatterns runs the in-field masking rules for a single field value.
func (fc *fieldCrypto) applyPatterns(field, value string, patterns []compiledPattern) string {
	out := value
	for _, p := range patterns {
		switch p.typ {
		case patternTypeCPF:
			// Mask only CANDIDATES that pass the two-stage CPF check; leave invalid
			// CPF-shaped numbers (and everything else) intact.
			out = p.re.ReplaceAllStringFunc(out, func(m string) string {
				if isValidCPF(m) {
					return fc.maskValue
				}
				return m
			})
		case patternTypeCNPJ:
			out = p.re.ReplaceAllStringFunc(out, func(m string) string {
				if isValidCNPJ(m) {
					return fc.maskValue
				}
				return m
			})
		case patternTypeIBAN:
			out = p.re.ReplaceAllStringFunc(out, func(m string) string {
				if isValidIBAN(m) {
					return fc.maskValue
				}
				return m
			})
		case patternTypeRegex:
			out = p.re.ReplaceAllString(out, fc.maskValue)
		}
	}
	return out
}

// processMap applies encrypt/mask/pattern rules to every string value in m. Encryption
// key material is fetched lazily via keyFn and the resulting key id is written back into
// m under keyIDAttr. Adding that attribute is deferred until after Range to avoid mutating
// the map during iteration.
func (fc *fieldCrypto) processMap(ctx context.Context, m pcommon.Map) {
	var encKeyID string

	m.Range(func(k string, v pcommon.Value) bool {
		if v.Type() != pcommon.ValueTypeStr {
			return true // only operate on string-typed values
		}
		s := v.Str()
		switch {
		case contains(fc.encryptFields, k):
			id, ct, ok := fc.encryptValue(ctx, s)
			if ok {
				v.SetStr(ct)
				encKeyID = id
			}
		case contains(fc.maskFields, k):
			v.SetStr(fc.maskValue)
		case fc.maskTemplates[k] != "":
			v.SetStr(maskWithTemplate(s, fc.maskTemplates[k]))
		default:
			if pats, ok := fc.patternsByField[k]; ok {
				v.SetStr(fc.applyPatterns(k, s, pats))
			}
		}
		return true
	})

	if encKeyID != "" {
		m.PutStr(keyIDAttr, encKeyID)
	}
}

// processBody handles the log record body when it is a string, plus records the key id
// (when the body is encrypted) into the record's attribute map.
func (fc *fieldCrypto) processBody(ctx context.Context, body pcommon.Value, recordAttrs pcommon.Map) {
	if body.Type() != pcommon.ValueTypeStr {
		return
	}
	s := body.Str()
	const bodyKey = "body"
	switch {
	case contains(fc.encryptFields, bodyKey):
		if id, ct, ok := fc.encryptValue(ctx, s); ok {
			body.SetStr(ct)
			recordAttrs.PutStr(keyIDAttr, id)
		}
	case contains(fc.maskFields, bodyKey):
		body.SetStr(fc.maskValue)
	case fc.maskTemplates[bodyKey] != "":
		body.SetStr(maskWithTemplate(s, fc.maskTemplates[bodyKey]))
	default:
		if pats, ok := fc.patternsByField[bodyKey]; ok {
			body.SetStr(fc.applyPatterns(bodyKey, s, pats))
		}
	}
}

// encryptValue encrypts one string with the current key. It NEVER logs plaintext or key
// material; on error it logs the field-independent failure and leaves the value untouched.
func (fc *fieldCrypto) encryptValue(ctx context.Context, plaintext string) (keyID, ciphertext string, ok bool) {
	id, key, err := fc.kp.CurrentKey(ctx)
	if err != nil {
		fc.logger.Error("fieldcrypto: could not obtain current key", zap.Error(err))
		return "", "", false
	}
	ct, err := EncryptAESGCM(key, []byte(plaintext))
	if err != nil {
		fc.logger.Error("fieldcrypto: encryption failed", zap.String("key_id", id), zap.Error(err))
		return "", "", false
	}
	return id, ct, true
}

func contains(set map[string]struct{}, k string) bool {
	_, ok := set[k]
	return ok
}

// ─── LOGS ────────────────────────────────────────────────────────────────────────

func (fc *fieldCrypto) processLogs(ctx context.Context, ld plog.Logs) {
	rls := ld.ResourceLogs()
	for i := 0; i < rls.Len(); i++ {
		rl := rls.At(i)
		fc.processMap(ctx, rl.Resource().Attributes())
		sls := rl.ScopeLogs()
		for j := 0; j < sls.Len(); j++ {
			lrs := sls.At(j).LogRecords()
			for k := 0; k < lrs.Len(); k++ {
				lr := lrs.At(k)
				fc.processMap(ctx, lr.Attributes())
				fc.processBody(ctx, lr.Body(), lr.Attributes())
			}
		}
	}
}

// ─── TRACES (attribute-level only) ─────────────────────────────────────────────────

func (fc *fieldCrypto) processTraces(ctx context.Context, td ptrace.Traces) {
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		fc.processMap(ctx, rs.Resource().Attributes())
		sss := rs.ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			spans := sss.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				fc.processMap(ctx, spans.At(k).Attributes())
			}
		}
	}
}

// ─── METRICS (attribute-level only) ─────────────────────────────────────────────────

func (fc *fieldCrypto) processMetrics(ctx context.Context, md pmetric.Metrics) {
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		fc.processMap(ctx, rm.Resource().Attributes())
		sms := rm.ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			metrics := sms.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				fc.processDataPointAttrs(ctx, metrics.At(k))
			}
		}
	}
}

func (fc *fieldCrypto) processDataPointAttrs(ctx context.Context, m pmetric.Metric) {
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		dps := m.Gauge().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			fc.processMap(ctx, dps.At(i).Attributes())
		}
	case pmetric.MetricTypeSum:
		dps := m.Sum().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			fc.processMap(ctx, dps.At(i).Attributes())
		}
	case pmetric.MetricTypeHistogram:
		dps := m.Histogram().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			fc.processMap(ctx, dps.At(i).Attributes())
		}
	}
}

// ─── signal-specific processor wrappers ────────────────────────────────────────────

type logsProcessor struct {
	*fieldCrypto
	next consumer.Logs
}

func (p *logsProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}
func (p *logsProcessor) Start(ctx context.Context, _ component.Host) error { return p.start(ctx) }
func (p *logsProcessor) Shutdown(context.Context) error                    { return nil }
func (p *logsProcessor) ConsumeLogs(ctx context.Context, ld plog.Logs) error {
	p.processLogs(ctx, ld)
	return p.next.ConsumeLogs(ctx, ld)
}

type tracesProcessor struct {
	*fieldCrypto
	next consumer.Traces
}

func (p *tracesProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}
func (p *tracesProcessor) Start(ctx context.Context, _ component.Host) error { return p.start(ctx) }
func (p *tracesProcessor) Shutdown(context.Context) error                    { return nil }
func (p *tracesProcessor) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	p.processTraces(ctx, td)
	return p.next.ConsumeTraces(ctx, td)
}

type metricsProcessor struct {
	*fieldCrypto
	next consumer.Metrics
}

func (p *metricsProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}
func (p *metricsProcessor) Start(ctx context.Context, _ component.Host) error { return p.start(ctx) }
func (p *metricsProcessor) Shutdown(context.Context) error                    { return nil }
func (p *metricsProcessor) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	p.processMetrics(ctx, md)
	return p.next.ConsumeMetrics(ctx, md)
}

// compile-time interface assertions
var (
	_ processor.Logs    = (*logsProcessor)(nil)
	_ processor.Traces  = (*tracesProcessor)(nil)
	_ processor.Metrics = (*metricsProcessor)(nil)
)
