package cli

// configedit.go changes named values in cockpit.config.json and leaves everything else as the
// operator wrote it: other keys, their order, and keys this build does not know. The operator
// commands that write configuration (keygen binding an identity, pin, trust) go through it, so a
// command that sets one value cannot quietly drop another.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

type member struct {
	key string
	val json.RawMessage
}

// object is a JSON object that keeps its members in order.
type object []member

func parseObject(b []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("not a JSON object")
	}
	var o object
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o = append(o, member{t.(string), v})
	}
	return o, nil
}

func (o object) marshal() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{")
	for i, m := range o {
		if i > 0 {
			buf.WriteString(",")
		}
		k, _ := json.Marshal(m.key)
		buf.WriteString(string(k) + ":")
		buf.Write(m.val)
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// set puts value at path, creating objects on the way; an existing member keeps its place.
func setPath(doc []byte, path []string, value any) ([]byte, error) {
	o, err := parseObject(doc)
	if err != nil {
		return nil, err
	}
	var v json.RawMessage
	if len(path) == 1 {
		if v, err = json.Marshal(value); err != nil {
			return nil, err
		}
	} else {
		inner := []byte("{}")
		for _, m := range o {
			if m.key == path[0] && bytes.HasPrefix(bytes.TrimSpace(m.val), []byte("{")) {
				inner = m.val
			}
		}
		if v, err = setPath(inner, path[1:], value); err != nil {
			return nil, err
		}
	}
	for i := range o {
		if o[i].key == path[0] {
			o[i].val = v
			return o.marshal()
		}
	}
	return append(o, member{path[0], v}).marshal()
}

// editConfig applies the edits to the file in order and writes it back indented as init writes it.
func editConfig(cfgPath string, edits ...func([]byte) ([]byte, error)) error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	for _, e := range edits {
		if b, err = e(b); err != nil {
			return fmt.Errorf("%s: %w", cfgPath, err)
		}
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return err
	}
	out.WriteString("\n")
	return os.WriteFile(cfgPath, out.Bytes(), 0o644)
}

func set(path []string, value any) func([]byte) ([]byte, error) {
	return func(b []byte) ([]byte, error) { return setPath(b, path, value) }
}
