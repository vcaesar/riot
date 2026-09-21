//  Copyright (c) 2026 The Riot Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 		http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

func documentFields(field string, data any) (map[string][]string, error) {
	if text, ok := data.(string); ok {
		return map[string][]string{field: {text}}, nil
	}
	value := reflect.ValueOf(data)
	for value.IsValid() && value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected string or non-nil struct, got %T", data)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("error mapping struct: %v", err)
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("error decoding struct mapping: %v", err)
	}
	if object == nil {
		return nil, fmt.Errorf("struct mapping must encode a JSON object")
	}
	fields := make(map[string][]string)
	if err := flattenFields(fields, "", object); err != nil {
		return nil, err
	}
	return fields, nil
}

func flattenFields(fields map[string][]string, path string, value any) error {
	switch value := value.(type) {
	case map[string]any:
		for _, name := range slices.Sorted(maps.Keys(value)) {
			if name == "" || name[0] == '_' || strings.ContainsRune(name, '.') {
				return fmt.Errorf("invalid mapped field name %q", name)
			}
			child := name
			if path != "" {
				child = path + "." + name
			}
			if err := flattenFields(fields, child, value[name]); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range value {
			if err := flattenFields(fields, path, item); err != nil {
				return err
			}
		}
	case string:
		fields[path] = append(fields[path], value)
	case json.Number, bool:
		fields[path] = append(fields[path], fmt.Sprint(value))
	}
	return nil
}
