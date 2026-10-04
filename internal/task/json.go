package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MarshalJSON refuses to serialize an invalid plan.
func (p Plan) MarshalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	type wire Plan
	return json.Marshal(wire(p))
}

// UnmarshalJSON validates before replacing the receiver. Unknown fields and
// duplicate keys are rejected rather than silently discarding user data.
func (p *Plan) UnmarshalJSON(data []byte) error {
	if !utf8.Valid(data) {
		return invalid("JSON не является UTF-8")
	}
	if err := checkUnicodeEscapes(data); err != nil {
		return err
	}
	if err := checkJSON(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return fmt.Errorf("%w: JSON: %v", ErrInvalid, err)
	}
	type wire Plan
	var next wire
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&next); err != nil {
		return fmt.Errorf("%w: JSON: %v", ErrInvalid, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return invalid("лишние данные после JSON")
	}
	value := Plan(next)
	if err := value.Validate(); err != nil {
		return err
	}
	for i := range value.Tasks {
		value.Tasks[i].project("")
	}
	*p = value
	return nil
}

// checkUnicodeEscapes runs before decoding, which replaces unpaired UTF-16
// surrogates with U+FFFD. Valid JSON guarantees complete hexadecimal escapes;
// skipping each escaped character also leaves literal \\u text untouched.
func checkUnicodeEscapes(data []byte) error {
	if !json.Valid(data) {
		return invalid("некорректный JSON")
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		code, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		i += 4
		switch {
		case code >= 0xd800 && code <= 0xdbff:
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return invalid("непарный старший суррогат Unicode")
			}
			low, _ := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if low < 0xdc00 || low > 0xdfff {
				return invalid("непарный старший суррогат Unicode")
			}
			i += 6
		case code >= 0xdc00 && code <= 0xdfff:
			return invalid("непарный младший суррогат Unicode")
		}
	}
	return nil
}

// checkJSON walks tokens to detect duplicate keys and noncanonical optional
// fields that encoding/json would otherwise collapse into an absent value.
func checkJSON(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("null не поддерживается; неизвестное поле должно отсутствовать")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name := key.(string)
			if strings.ToLower(name) != name {
				return fmt.Errorf("ключ %q должен иметь точное написание в нижнем регистре", name)
			}
			if seen[name] {
				return fmt.Errorf("повтор ключа %q", name)
			}
			seen[name] = true
			if name == "started_at" || name == "completed_at" || name == "observed_at" {
				var value string
				if err := dec.Decode(&value); err != nil {
					return err
				}
				if !validJSONTime(value) {
					return fmt.Errorf("некорректная дата %s", name)
				}
			} else if name == "attempts" || name == "warnings" || name == "rebindings" {
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return err
				}
				var entries []json.RawMessage
				if err := json.Unmarshal(raw, &entries); err != nil {
					return err
				}
				if len(entries) == 0 {
					return fmt.Errorf("пустое %s должно отсутствовать", name)
				}
				if err := checkJSON(json.NewDecoder(bytes.NewReader(raw))); err != nil {
					return err
				}
			} else if err := checkJSON(dec); err != nil {
				return err
			}
		}
		if seen["format"] && (!seen["revision"] || !seen["last_event"]) || seen["title"] && seen["status"] && !seen["revision"] {
			return fmt.Errorf("отсутствует обязательный счётчик")
		}
	case '[':
		for dec.More() {
			if err := checkJSON(dec); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("неожиданный разделитель")
	}
	_, err = dec.Token()
	return err
}
