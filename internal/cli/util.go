package cli

import (
	"fmt"
	"reflect"

	"github.com/spf13/cobra"
)

// BindFlagsFromStruct 使用反射读取结构体字段的 tag，自动在 cobra.Command 上绑定 flag。
//
// 支持的字段类型：
//   - string      -> StringVar
//   - int         -> IntVar
//   - bool        -> BoolVar
//   - []string    -> StringArrayVar （需要在 tag 中加 multi:"true"）
//
// 支持的 tag：
//   - flag    : flag 名称（必填）
//   - default : 默认值（字符串，需要根据字段类型做转换）
//   - usage   : 帮助说明
//   - multi   : 针对 []string，设置为 "true" 表示允许重复出现
func BindFlagsFromStruct(cmd *cobra.Command, target any) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("BindFlagsFromStruct: target must be pointer to struct")
	}

	v = v.Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		flagName := field.Tag.Get("flag")
		if flagName == "" {
			// 没有 flag tag 的字段直接跳过
			continue
		}
		defaultStr := field.Tag.Get("default")
		usage := field.Tag.Get("usage")
		multi := field.Tag.Get("multi") == "true"

		switch value.Kind() {
		case reflect.String:
			def := defaultStr
			if def == "" {
				def = value.String()
			}
			cmd.Flags().StringVar(value.Addr().Interface().(*string), flagName, def, usage)

		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			// 这里只处理普通 int，其他 int 类型可以按需扩展
			if value.Kind() != reflect.Int {
				return fmt.Errorf("BindFlagsFromStruct: unsupported int kind %s on field %s", value.Kind(), field.Name)
			}
			def := int(value.Int())
			if defaultStr != "" {
				var parsed int
				if _, err := fmt.Sscanf(defaultStr, "%d", &parsed); err != nil {
					return fmt.Errorf("BindFlagsFromStruct: parse default for field %s: %w", field.Name, err)
				}
				def = parsed
			}
			cmd.Flags().IntVar(value.Addr().Interface().(*int), flagName, def, usage)

		case reflect.Bool:
			def := value.Bool()
			if defaultStr != "" {
				var parsed bool
				if _, err := fmt.Sscanf(defaultStr, "%t", &parsed); err != nil {
					return fmt.Errorf("BindFlagsFromStruct: parse default for field %s: %w", field.Name, err)
				}
				def = parsed
			}
			cmd.Flags().BoolVar(value.Addr().Interface().(*bool), flagName, def, usage)

		case reflect.Slice:
			if value.Type().Elem().Kind() != reflect.String {
				return fmt.Errorf("BindFlagsFromStruct: only []string slices are supported (field %s)", field.Name)
			}
			var def []string
			if defaultStr != "" {
				// 简单起见，这里只支持单个默认值；如果需要多值可以自己在 default 里约定分隔符再扩展解析逻辑
				def = []string{defaultStr}
			} else if value.Len() > 0 {
				def = make([]string, value.Len())
				for i := 0; i < value.Len(); i++ {
					def[i] = value.Index(i).String()
				}
			}

			if !multi {
				// 对于 []string，但不需要多次出现的场景，也可以用 StringSliceVar / StringArrayVar，
				// 这里统一用 StringArrayVar，行为和多次出现时一致。
			}
			cmd.Flags().StringArrayVar(value.Addr().Interface().(*[]string), flagName, def, usage)

		default:
			return fmt.Errorf("BindFlagsFromStruct: unsupported field type %s on %s", field.Type, field.Name)
		}
	}

	return nil
}
