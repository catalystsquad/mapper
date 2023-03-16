package pkg

import (
	"encoding/json"
	"github.com/joomcode/errorx"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"reflect"
	"strings"
)

const mapperTagName = "mapper"
const jsonTagName = "json"

type tagInfo struct {
	MapperFieldPath string
	AsString        bool
	Field           reflect.StructField
	JsonFieldName   string
}

func Marshal(v any) ([]byte, error) {
	if isSlice(v) {
		return marshalSlice(v)
	}
	if isStruct(v) {
		return marshalStruct(v)
	}
	return nil, errorx.IllegalArgument.New("unsupported type")
}

func marshalSlice(v any) ([]byte, error) {
	marshalledString := "["
	sliceValue := reflect.ValueOf(v)
	if sliceValue.Kind() == reflect.Ptr {
		sliceValue = sliceValue.Elem()
	}
	for i := 0; i < sliceValue.Len(); i++ {
		structBytes, err := marshalStruct(sliceValue.Index(i).Interface())
		if err != nil {
			return nil, err
		}
		marshalledString += string(structBytes)
		if i+1 < sliceValue.Len() {
			marshalledString += ","
		}
	}
	marshalledString += "]"
	return []byte(marshalledString), nil
}

func marshalStruct(v any) ([]byte, error) {
	// read tags
	tagDatas, err := getTagDatas(v)
	if err != nil {
		return nil, err
	}
	//marshall to json first
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	changes := map[string]interface{}{}
	// process any fields that have the mapper tag, track updates in case there is collision on tags
	if len(tagDatas) > 0 {
		for _, tagData := range tagDatas {
			// get the value from the json marshalled data
			value, err := getValue(jsonBytes, tagData.JsonFieldName, tagData.AsString, tagData.Field.Type.Kind())
			if err != nil {
				return nil, err
			}
			changes[tagData.MapperFieldPath] = value
		}
		// apply updates
		for path, value := range changes {
			// set the value at the mapped path
			jsonBytes, err = sjson.SetBytes(jsonBytes, path, value)
			if err != nil {
				return nil, err
			}
		}
	}

	return jsonBytes, nil
}

func Unmarshal(data []byte, v interface{}) error {
	vValue := reflect.ValueOf(v)
	if vValue.IsNil() || vValue.Kind() != reflect.Ptr {
		return errorx.IllegalArgument.New("Cannot Unmarshal to nil or non pointer")
	}
	if isSlice(v) {
		return unmarshalSlice(data, v)
	}
	if isStruct(v) {
		return unmarshalStruct(data, v)
	}
	return errorx.IllegalArgument.New("unsupported type")
}

func unmarshalSlice(data []byte, v interface{}) (err error) {
	sliceObjType := reflect.TypeOf(v).Elem().Elem()
	gjson.GetBytes(data, "@this").ForEach(func(key, value gjson.Result) bool {
		var newObj interface{}
		if sliceObjType.Kind() == reflect.Ptr {
			newObj = reflect.New(sliceObjType.Elem()).Interface()
		} else {
			newObj = reflect.New(sliceObjType).Interface()
		}
		err = unmarshalStruct([]byte(value.String()), newObj)
		if err != nil {
			return false
		}
		if sliceObjType.Kind() == reflect.Struct && reflect.TypeOf(newObj).Kind() == reflect.Ptr {
			appendToSlice(v, reflect.ValueOf(newObj).Elem().Interface())
		} else {
			appendToSlice(v, reflect.ValueOf(newObj).Interface())
		}
		return true
	})
	return
}

func unmarshalStruct(data []byte, v any) error {
	// read tags
	tagDatas, err := getTagDatas(v)
	if err != nil {
		return err
	}

	// process any fields that have the mapper tag, track updates in case there is collision on tags
	if len(tagDatas) > 0 {
		changes := map[string]interface{}{}
		for _, tagData := range tagDatas {
			// get the value using the mapped path
			value, err := getValue(data, tagData.MapperFieldPath, tagData.AsString, tagData.Field.Type.Kind())
			if err != nil {
				return err
			}
			changes[tagData.JsonFieldName] = value
		}
		// apply updates
		for path, value := range changes {
			// set the value to the field's json path
			data, err = sjson.SetBytes(data, path, value)
			if err != nil {
				return err
			}
		}
	}
	err = json.Unmarshal(data, v)
	return err
}

func getTagDatas(v any) ([]tagInfo, error) {
	// map the marshal fields
	destType := reflect.TypeOf(v)
	if destType.Kind() == reflect.Ptr {
		// if it's a pointer, get the non pointer type
		destType = destType.Elem()
	}

	// Iterate over all available fields and read the tag values
	tagDatas := []tagInfo{}
	for i := 0; i < destType.NumField(); i++ {
		field := destType.Field(i)
		tagData := getTagInfo(field)
		if tagData.MapperFieldPath != "" {
			tagDatas = append(tagDatas, tagData)
		}
	}
	return tagDatas, nil
}

func getValue(data []byte, path string, asString bool, typ reflect.Kind) (interface{}, error) {
	result := gjson.GetBytes(data, path)
	if asString {
		// the json tag indicates it should be a string value, so the value is the string of the result
		return result.String(), nil
	} else {
		// the json tag does not indicate it should be a string, so type switch to use the correct type
		switch typ {
		case reflect.String:
			return result.String(), nil
		case reflect.Bool:
			return result.Bool(), nil
		case reflect.Int:
			return int(result.Int()), nil
		case reflect.Int8:
			return int8(result.Int()), nil
		case reflect.Int16:
			return int16(result.Int()), nil
		case reflect.Int32:
			return int32(result.Int()), nil
		case reflect.Int64:
			return result.Int(), nil
		case reflect.Uint:
			return uint(result.Uint()), nil
		case reflect.Uint8:
			return uint8(result.Uint()), nil
		case reflect.Uint16:
			return uint16(result.Uint()), nil
		case reflect.Uint32:
			return uint32(result.Uint()), nil
		case reflect.Uint64:
			return result.Uint(), nil
		case reflect.Float32:
			return float32(result.Float()), nil
		case reflect.Float64:
			return result.Float(), nil
		default:
			return nil, errorx.IllegalState.New("unsupported type: %s", typ)
		}
	}
}

func getTagInfo(field reflect.StructField) tagInfo {
	tagData := tagInfo{
		Field: field,
	}
	mapperTagSplit := strings.Split(field.Tag.Get(mapperTagName), ",")
	if len(mapperTagSplit) > 0 {
		tagData.MapperFieldPath = mapperTagSplit[0]
	}
	if len(mapperTagSplit) > 1 && mapperTagSplit[1] == "string" {
		tagData.AsString = true
	}
	jsonTagSplit := strings.Split(field.Tag.Get(jsonTagName), ",")
	if len(jsonTagSplit) > 0 {
		tagData.JsonFieldName = jsonTagSplit[0]
	} else {
		tagData.JsonFieldName = field.Name
	}
	return tagData
}

func isSlice(v any) bool {
	_, typ := getValueAndType(v)
	return typ.Kind() == reflect.Slice
}

func isStruct(v any) bool {
	_, typ := getValueAndType(v)
	return typ.Kind() == reflect.Struct
}
func getValueAndType(v any) (value reflect.Value, typ reflect.Type) {
	typ = reflect.TypeOf(v)
	value = reflect.ValueOf(v)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		value = value.Elem()
	}
	return
}

func appendToSlice(arrPtr, toAppend interface{}) {
	valuePtr := reflect.ValueOf(arrPtr)
	value := valuePtr.Elem()
	value.Set(reflect.Append(value, reflect.ValueOf(toAppend)))
}
