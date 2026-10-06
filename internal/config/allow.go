package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AllowPackage adds a package id to claims.allowedPackages in the configuration file at root and
// reports whether it changed anything. It is the one write the cockpit makes to its own
// configuration, and only on the operator's explicit request (`cockpit install --allow`): admitting
// a package is a trust decision, and the configuration is where trust decisions live.
//
// The file is rewritten with its keys in their original order and two-space indentation, so the
// change is the one line that matters and not a reformatted file.
func AllowPackage(root, id string) (bool, error) {
	path := filepath.Join(root, "cockpit.config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc, err := decodeOrdered(json.NewDecoder(bytes.NewReader(raw)))
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	top, ok := doc.(*object)
	if !ok {
		return false, fmt.Errorf("%s is not a JSON object", path)
	}
	claims, ok := top.get("claims").(*object)
	if !ok {
		claims = &object{}
		top.set("claims", claims)
	}
	list, _ := claims.get("allowedPackages").([]any)
	for _, v := range list {
		if v == id {
			return false, nil
		}
	}
	claims.set("allowedPackages", append(list, id))
	var b bytes.Buffer
	writeOrdered(&b, top, "")
	b.WriteString("\n")
	return true, os.WriteFile(path, b.Bytes(), 0o644)
}

// object is a JSON object that keeps its key order.
type object struct {
	keys []string
	vals map[string]any
}

func (o *object) get(k string) any { return o.vals[k] }

func (o *object) set(k string, v any) {
	if o.vals == nil {
		o.vals = map[string]any{}
	}
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func decodeOrdered(d *json.Decoder) (any, error) {
	d.UseNumber()
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := &object{}
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, err
				}
				v, err := decodeOrdered(d)
				if err != nil {
					return nil, err
				}
				o.set(kt.(string), v)
			}
			_, err := d.Token()
			return o, err
		case '[':
			list := []any{}
			for d.More() {
				v, err := decodeOrdered(d)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			_, err := d.Token()
			return list, err
		}
	}
	return tok, nil
}

func writeOrdered(b *bytes.Buffer, v any, indent string) {
	inner := indent + "  "
	switch x := v.(type) {
	case *object:
		if len(x.keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, k := range x.keys {
			kb, _ := json.Marshal(k)
			b.WriteString(inner)
			b.Write(kb)
			b.WriteString(": ")
			writeOrdered(b, x.vals[k], inner)
			if i < len(x.keys)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indent + "}")
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range x {
			b.WriteString(inner)
			writeOrdered(b, e, inner)
			if i < len(x)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indent + "]")
	case nil:
		b.WriteString("null")
	default:
		vb, _ := json.Marshal(x)
		b.Write(vb)
	}
}
