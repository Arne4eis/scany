package dbscan

import (
	"reflect"
	"strings"
)

type toTraverse struct {
	Type         reflect.Type
	IndexPrefix  []int
	ColumnPrefix string
	// Parent is the innermost struct by pointer that contains fields of Type, nil if there is none.
	Parent *ptrStructMeta
}

// fieldMeta describes the struct field that a column is mapped to.
type fieldMeta struct {
	index []int
	// parent is the innermost struct by pointer that contains the field, nil if there is none.
	parent *ptrStructMeta
}

// ptrStructMeta describes a nested or embedded struct field by pointer.
type ptrStructMeta struct {
	index []int
	// parent is the innermost struct by pointer that contains this struct, nil if there is none.
	parent *ptrStructMeta
}

func (api *API) getColumnToFieldIndexMap(structType reflect.Type) map[string]fieldMeta {
	resultIface, ok := api.columnToIndexFieldMapCache.Load(structType)
	if ok {
		return resultIface.(map[string]fieldMeta)
	}

	result := api.buildColumnToFieldIndexMap(structType)
	resultIface, _ = api.columnToIndexFieldMapCache.LoadOrStore(structType, result)
	result = resultIface.(map[string]fieldMeta)
	return result
}

func (api *API) buildColumnToFieldIndexMap(structType reflect.Type) map[string]fieldMeta {
	result := make(map[string]fieldMeta, structType.NumField())
	var queue []*toTraverse
	queue = append(queue, &toTraverse{Type: structType, IndexPrefix: nil, ColumnPrefix: ""})
	for len(queue) > 0 {
		traversal := queue[0]
		queue = queue[1:]
		structType := traversal.Type
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)

			if field.PkgPath != "" && !field.Anonymous {
				// Field is unexported, skip it.
				continue
			}

			dbTag, dbTagPresent := field.Tag.Lookup(api.structTagKey)
			if dbTagPresent {
				dbTag = strings.Split(dbTag, ",")[0]
			}
			if dbTag == "-" {
				// Field is ignored, skip it.
				continue
			}

			index := make([]int, 0, len(traversal.IndexPrefix)+len(field.Index))
			index = append(index, traversal.IndexPrefix...)
			index = append(index, field.Index...)

			columnPart := dbTag
			if !dbTagPresent {
				columnPart = api.fieldMapperFn(field.Name)
			}
			if !field.Anonymous {
				column := api.buildColumn(traversal.ColumnPrefix, columnPart)

				if _, exists := result[column]; !exists {
					result[column] = fieldMeta{index: index, parent: traversal.Parent}
				}
			}

			childType := field.Type
			if field.Type.Kind() == reflect.Ptr {
				childType = field.Type.Elem()
			}
			if childType.Kind() == reflect.Struct {
				if field.Anonymous {
					// If "db" tag is present for embedded struct
					// use it with "." to prefix all column from the embedded struct.
					// the default behavior is to propagate columns as is.
					columnPart = dbTag
				}
				columnPrefix := api.buildColumn(traversal.ColumnPrefix, columnPart)
				parent := traversal.Parent
				if field.Type.Kind() == reflect.Ptr {
					parent = &ptrStructMeta{index: index, parent: parent}
				}
				queue = append(queue, &toTraverse{
					Type:         childType,
					IndexPrefix:  index,
					ColumnPrefix: columnPrefix,
					Parent:       parent,
				})
			}
		}
	}

	return result
}

func (api *API) buildColumn(parts ...string) string {
	var notEmptyParts []string
	for _, p := range parts {
		if p != "" {
			notEmptyParts = append(notEmptyParts, p)
		}
	}
	return strings.Join(notEmptyParts, api.columnSeparator)
}

func initializeNested(structValue reflect.Value, fieldIndex []int) {
	i := fieldIndex[0]
	field := structValue.Field(i)

	// Create a new instance of a struct and set it to field,
	// if field is a nil pointer to a struct.
	if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Struct && field.IsNil() {
		field.Set(reflect.New(field.Type().Elem()))
	}
	if len(fieldIndex) > 1 {
		initializeNested(reflect.Indirect(field), fieldIndex[1:])
	}
}
