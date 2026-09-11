package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// Decode rejects duplicate keys, unknown/case-folded fields, and trailing input.
func Decode(data []byte, out any) error {
	t := reflect.TypeOf(out)
	if t == nil || t.Kind() != reflect.Pointer {
		return fmt.Errorf("JSON target must be pointer")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := scan(d, 0, t.Elem()); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON input")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func scan(d *json.Decoder, depth int, target reflect.Type) error {
	for target != nil && target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if target == reflect.TypeOf(json.RawMessage{}) {
		target = nil
	}
	if depth > 64 {
		return fmt.Errorf("JSON nesting too deep")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		fields := map[string]reflect.Type{}
		if target != nil {
			switch target.Kind() {
			case reflect.Struct:
				for i := 0; i < target.NumField(); i++ {
					f := target.Field(i)
					if !f.IsExported() {
						continue
					}
					name := strings.Split(f.Tag.Get("json"), ",")[0]
					if name == "-" {
						continue
					}
					if name == "" {
						name = f.Name
					}
					fields[name] = f.Type
				}
			case reflect.Map:
			default:
				return fmt.Errorf("unexpected JSON object")
			}
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate/invalid JSON key")
			}
			seen[name] = true
			var child reflect.Type
			if target != nil {
				if target.Kind() == reflect.Map {
					child = target.Elem()
				} else {
					var ok bool
					child, ok = fields[name]
					if !ok {
						return fmt.Errorf("unknown JSON field %q", name)
					}
				}
			}
			if err := scan(d, depth+1, child); err != nil {
				return err
			}
		}
	case '[':
		var child reflect.Type
		if target != nil {
			if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
				return fmt.Errorf("unexpected JSON array")
			}
			child = target.Elem()
		}
		for d.More() {
			if err := scan(d, depth+1, child); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	_, err = d.Token()
	return err
}
