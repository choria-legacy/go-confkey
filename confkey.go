// Package confkey looks for tags on a structure and set values
// based on the tag rather than the struct item names
//
// Features
//
// Defaults are supported and can be fetched from the shell environment
//
// The tags can specify some formating like comma splits and other
// commonly seen patterns in config files.
//
// Conversion of []string, ints, strings, time.Duration and booleans are support
//
// Validations can be done on a struct basis using the github.com/choria-io/go-validators
// package
//
// A sample structure might look like this, the package contains utilities to
// set values, apply defaults and perform validations
//
//    type Config struct {
//        Loglevel string        `confkey:"loglevel" default:"warn" validate:"enum=debug,info,warn,error"`
//        Mode     string        `confkey:"mode" default:"server" validate:"enum=server,client"`
//        Servers  []string      `confkey:"servers" type:"comma_split" environment:"SERVERS"`
//        Path     []string      `confkey:"path" type:"path_split" default:"/bin:/usr/bin"`
//        I        time.Duration `confkey:"interval" type:"duration" default:"1h"`
//    }
//
// The utilities here will let you parse any config file that might have keys like loglevel etc
// and map the string values read from the text file onto the structure
package confkey

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	validator "github.com/choria-io/go-validator"
	"github.com/oleiade/reflections"
)

// Validate validates the struct
func Validate(target interface{}) error {
	_, err := validator.ValidateStruct(target)

	return err
}

// SetStructDefaults extract defaults out of the tags and set them to the key
func SetStructDefaults(target interface{}) error {
	if reflect.TypeOf(target).Kind() != reflect.Ptr {
		return errors.New("pointer is required")
	}

	fields, err := reflections.Fields(target)
	if err != nil {
		return err
	}

	for _, field := range fields {
		confkey, _ := reflections.GetFieldTag(target, field, "confkey")
		deflt, _ := reflections.GetFieldTag(target, field, "default")

		if confkey != "" && deflt != "" {
			err = SetStructFieldWithKey(target, confkey, deflt)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// StringFieldWithKey retrieves a string from target that matches key, "" when not found
func StringFieldWithKey(target interface{}, key string) string {
	val, kind, err := getFieldValAndKind(target, key)
	if err != nil {
		return ""
	}

	if kind == reflect.String {
		v, ok := val.(string)
		if ok {
			return v
		}
	}

	return ""
}

// StringListWithKey retrieves a []string from target that matches key, empty when not found
func StringListWithKey(target interface{}, key string) []string {
	val, kind, err := getFieldValAndKind(target, key)
	if err != nil {
		return []string{}
	}

	if kind == reflect.Slice {
		v, ok := val.([]string)
		if ok {
			return v
		}
	}

	return []string{}
}

// BoolWithKey retrieves a bool from target that matches key, false when not found
func BoolWithKey(target interface{}, key string) bool {
	val, kind, err := getFieldValAndKind(target, key)
	if err != nil {
		return false
	}

	if kind == reflect.Bool {
		v, ok := val.(bool)
		if ok {
			return v
		}
	}

	return false
}

// IntWithKey retrieves an int from target that matches key, 0 when not found
func IntWithKey(target interface{}, key string) int {
	val, kind, err := getFieldValAndKind(target, key)
	if err != nil {
		return 0
	}

	if kind == reflect.Int {
		v, ok := val.(int)
		if ok {
			return v
		}
	}

	return 0
}

// Int64WithKey retrieves an int from target that matches key, 0 when not found
func Int64WithKey(target interface{}, key string) int64 {
	val, kind, err := getFieldValAndKind(target, key)
	if err != nil {
		return 0
	}

	if kind == reflect.Int64 {
		return val.(int64)
	}

	return 0
}

func getFieldValAndKind(target interface{}, key string) (interface{}, reflect.Kind, error) {
	item, err := fieldWithKey(target, key)
	if err != nil {
		return nil, reflect.Invalid, err
	}

	val, err := reflections.GetField(target, item)
	if err != nil {
		return nil, reflect.Invalid, err
	}

	kind, err := reflections.GetFieldKind(target, item)
	if err != nil {
		return nil, reflect.Invalid, err
	}

	return val, kind, nil
}

// SetStructFieldWithKey finds the struct key that matches the confkey on target and assign the value to it
func SetStructFieldWithKey(target interface{}, key string, value interface{}) error {
	if reflect.TypeOf(target).Kind() != reflect.Ptr {
		return errors.New("pointer is required")
	}

	item, err := fieldWithKey(target, key)
	if err != nil {
		return err
	}

	if tag, ok := tag(target, item, "environment"); ok {
		if v, ok := os.LookupEnv(tag); ok {
			value = v
		}
	}

	field := reflect.ValueOf(target).Elem().FieldByName(item)

	switch field.Kind() {
	case reflect.Slice:
		ptr := field.Addr().Interface().(*[]string)

		if tag, ok := tag(target, item, "type"); ok {
			switch tag {
			case "comma_split":
				// specifically clear it since these are one line split like 'collectives'
				*ptr = []string{}
				vals := strings.Split(value.(string), ",")

				for _, v := range vals {
					*ptr = append(*ptr, strings.TrimSpace(v))
				}

			case "colon_split":
				// these are like libdir, but we want to always use : to split and not
				// os path like path_split would do
				vals := strings.Split(value.(string), ":")

				for _, v := range vals {
					*ptr = append(*ptr, strings.TrimSpace(v))
				}

			case "path_split":
				// these are like libdir, either a one line split or a multiple occurance with splits
				vals := strings.Split(value.(string), string(os.PathListSeparator))

				for _, v := range vals {
					*ptr = append(*ptr, strings.TrimSpace(v))
				}
			}
		} else {
			*ptr = append(*ptr, strings.TrimSpace(value.(string)))
		}

	case reflect.Int:
		ptr := field.Addr().Interface().(*int)
		i, err := strconv.Atoi(value.(string))
		if err != nil {
			return err
		}
		*ptr = i

	case reflect.Int64:
		if tag, ok := tag(target, item, "type"); ok {
			if tag == "duration" {
				ptr := field.Addr().Interface().(*time.Duration)

				intonly, err := regexp.MatchString("\\A\\d+\\z", value.(string))
				if err != nil {
					return err
				}

				if intonly {
					i, err := strconv.Atoi(value.(string))
					if err != nil {
						return err
					}

					*ptr = time.Second * time.Duration(i)

					break
				}

				d, err := time.ParseDuration(value.(string))
				if err != nil {
					return err
				}

				*ptr = d
			}
		}

	case reflect.String:
		ptr := field.Addr().Interface().(*string)
		*ptr = value.(string)

		if tag, ok := tag(target, item, "type"); ok {
			if tag == "title_string" {
				a := []rune(value.(string))
				a[0] = unicode.ToUpper(a[0])
				*ptr = string(a)
			}
		}

	case reflect.Bool:
		ptr := field.Addr().Interface().(*bool)
		b, _ := strToBool(value.(string))
		*ptr = b
	}

	_, err = validator.ValidateStructField(target, item)

	return err
}

// determines the struct key name that is tagged with a certain confkey
func fieldWithKey(s interface{}, key string) (string, error) {
	fields, err := reflections.Fields(s)
	if err != nil {
		return "", fmt.Errorf("can't find any structure element configured with confkey '%s'", key)
	}

	for _, field := range fields {
		confkey, _ := tag(s, field, "confkey")
		if confkey == key {
			return field, nil
		}
	}

	return "", fmt.Errorf("can't find any structure element configured with confkey '%s'", key)
}

// retrieve a tag for a struct field
func tag(s interface{}, field string, tag string) (string, bool) {
	val, err := reflections.GetFieldTag(s, field, tag)

	return val, err == nil
}

// StrToBool converts a typical boolianish string to bool.
//
// 1, yes, true, y, t will be true
// 0, no, false, n, f will be false
// anything else will be false with an error
func strToBool(s string) (bool, error) {
	clean := strings.TrimSpace(s)

	if regexp.MustCompile(`(?i)^(1|yes|true|y|t)$`).MatchString(clean) {
		return true, nil
	}

	if regexp.MustCompile(`(?i)^(0|no|false|n|f)$`).MatchString(clean) {
		return false, nil
	}

	return false, errors.New("cannot convert string value '" + clean + "' into a boolean.")
}
