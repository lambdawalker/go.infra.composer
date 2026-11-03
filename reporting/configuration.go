package reporting

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Configuration holds allowlisted recorded outputs, independently of presence.
// ObservedAt is the state read time, not live verification of these settings.
// Values are canonical integer, boolean or enum text, never raw provider state.
type Configuration struct {
	Source     string            `json:"source"`
	ObservedAt time.Time         `json:"observedAt"`
	Values     map[string]string `json:"values"`
}

type configurationKind int

const (
	configurationInteger configurationKind = iota
	configurationBoolean
	configurationBillingMode
)

func configurationFields(typ string) map[string]configurationKind {
	switch typ {
	case "aws:lambda/function:Function":
		return map[string]configurationKind{"memorySize": configurationInteger, "timeout": configurationInteger, "reservedConcurrentExecutions": configurationInteger}
	case "aws:sqs/queue:Queue":
		return map[string]configurationKind{"visibilityTimeoutSeconds": configurationInteger, "messageRetentionSeconds": configurationInteger, "delaySeconds": configurationInteger, "receiveWaitTimeSeconds": configurationInteger, "maxMessageSize": configurationInteger}
	case "aws:cloudwatch/logGroup:LogGroup":
		return map[string]configurationKind{"retentionInDays": configurationInteger}
	case "aws:dynamodb/table:Table":
		return map[string]configurationKind{"billingMode": configurationBillingMode, "readCapacity": configurationInteger, "writeCapacity": configurationInteger, "deletionProtectionEnabled": configurationBoolean}
	}
	return nil
}

func configurationValue(value string, kind configurationKind) (string, bool) {
	switch kind {
	case configurationBoolean:
		return value, value == "true" || value == "false"
	case configurationBillingMode:
		return value, value == "PROVISIONED" || value == "PAY_PER_REQUEST"
	default:
		// ParseFloat rejects overflow/nonfinite JSON numbers. Rat then checks exact
		// integrality without rounding large fractional numbers into integers.
		if !json.Valid([]byte(value)) || value == "" || value[0] == '"' {
			return "", false
		}
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return "", false
		}
		if f == 0 {
			// Avoid constructing enormous denominators for underflowing exponents.
			mantissa := strings.FieldsFunc(value, func(r rune) bool { return r == 'e' || r == 'E' })[0]
			return "0", strings.Trim(mantissa, "-0.") == ""
		}
		n, ok := new(big.Rat).SetString(value)
		if !ok || !n.IsInt() {
			return "", false
		}
		return n.Num().String(), true
	}
}

func normalizeConfiguration(typ string, c *Configuration) *Configuration {
	if c == nil || c.Source != "pulumi-state" || c.ObservedAt.IsZero() {
		return nil
	}
	values := map[string]string{}
	for field, kind := range configurationFields(typ) {
		if value, ok := configurationValue(c.Values[field], kind); ok {
			values[field] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return &Configuration{Source: c.Source, ObservedAt: c.ObservedAt, Values: values}
}

// NewStateConfiguration extracts only known, plain output settings. Call only
// for recorded state; desired preview outputs are not observed configuration.
func NewStateConfiguration(typ string, outputs map[string]json.RawMessage, at time.Time) *Configuration {
	values := map[string]string{}
	for field, kind := range configurationFields(typ) {
		raw := outputs[field]
		value := strings.TrimSpace(string(raw))
		if kind == configurationBillingMode {
			if json.Unmarshal(raw, &value) != nil {
				continue
			}
		}
		if safe, ok := configurationValue(value, kind); ok {
			values[field] = safe
		}
	}
	return normalizeConfiguration(typ, &Configuration{Source: "pulumi-state", ObservedAt: at, Values: values})
}
