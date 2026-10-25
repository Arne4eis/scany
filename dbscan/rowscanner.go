package dbscan

import (
	"database/sql"
	"fmt"
	"reflect"
)

type startScannerFunc func(rs *RowScanner, dstValue reflect.Value) error

//go:generate mockery --name startScannerFunc --filename mock_test.go --inpackage

// RowScanner embraces Rows and exposes the Scan method
// that allows scanning data from the current row into the destination.
// The first time the Scan method is called
// it parses the destination type via reflection and caches all required information for further scans.
// Due to this caching mechanism, it's not allowed to call Scan for destinations of different types,
// the behavior is unknown in that case.
// RowScanner doesn't proceed to the next row nor close them, it should be done by the client code.
//
// The main benefit of using this type directly
// is that you can instantiate a RowScanner and manually iterate over the rows
// and control how data is scanned from each row.
// This can be beneficial if the result set is large
// and you don't want to allocate a slice for all rows at once
// as it would be done in ScanAll.
//
// ScanOne and ScanAll both use RowScanner type internally.
type RowScanner struct {
	api                *API
	rows               Rows
	columns            []string
	columnToFieldIndex map[string]fieldMeta
	mapElementType     reflect.Type
	started            bool
	scanFn             func(dstVal reflect.Value) error
	start              startScannerFunc
	scans              []any
	ptrStructs         []*ptrStructScan
	ptrStructColumns   []ptrStructColumn
}

// ptrStructScan holds the state of a struct by pointer for the current row, see WithNilStructPointers.
type ptrStructScan struct {
	index  []int
	parent *ptrStructScan
	// notNull is true if at least one column of the struct isn't NULL, including columns of nested structs.
	notNull bool
}

// ptrStructColumn is a column that belongs to a struct by pointer.
// It's scanned into a temporary value first to find out if the struct should be nil.
type ptrStructColumn struct {
	name   string
	pos    int
	index  []int
	parent *ptrStructScan
	// value is a pointer that the column is scanned into.
	// It has *T type for a field of type T and the field type for a field by pointer.
	value      reflect.Value
	fieldByPtr bool
}

// NewRowScanner is a package-level helper function that uses the DefaultAPI object.
// See API.NewRowScanner for details.
func NewRowScanner(rows Rows) *RowScanner {
	return DefaultAPI.NewRowScanner(rows)
}

// NewRowScanner returns a new instance of the RowScanner.
func (api *API) NewRowScanner(rows Rows) *RowScanner {
	return &RowScanner{
		api:   api,
		rows:  rows,
		start: startScanner,
	}
}

// Scan scans data from the current row into the destination.
// On the first call it caches expensive reflection work and uses it the future calls.
// See RowScanner for details.
func (rs *RowScanner) Scan(dst interface{}) error {
	dstVal, err := parseDestination(dst)
	if err != nil {
		return fmt.Errorf("parsing destination: %w", err)
	}
	if err := rs.doScan(dstVal); err != nil {
		return fmt.Errorf("doing scan: %w", err)
	}
	return nil
}

func (rs *RowScanner) doScan(dstValue reflect.Value) error {
	if !rs.started {
		if err := rs.start(rs, dstValue); err != nil {
			return fmt.Errorf("starting: %w", err)
		}
		rs.started = true
	}
	if err := rs.scanFn(dstValue); err != nil {
		return fmt.Errorf("scanFn: %w", err)
	}
	return nil
}

func startScanner(rs *RowScanner, dstValue reflect.Value) error {
	var err error
	rs.columns, err = rs.rows.Columns()
	if err != nil {
		return fmt.Errorf("scany: get rows columns: %w", err)
	}
	if err := rs.ensureDistinctColumns(); err != nil {
		return fmt.Errorf("duplicate columns: %w", err)
	}
	dstKind := dstValue.Kind()
	dstType := dstValue.Type()
	isScannable := rs.api.isScannableType(dstType)
	if isScannable && len(rs.columns) == 1 {
		rs.scanFn = rs.scanPrimitive
		return nil
	}

	if dstKind == reflect.Struct {
		rs.columnToFieldIndex = rs.api.getColumnToFieldIndexMap(dstType)
		if rs.api.nilStructPointers {
			rs.preparePtrStructs(dstType)
		}
		rs.scanFn = rs.scanStruct
		return nil
	}

	if dstKind == reflect.Map {
		if dstType.Key().Kind() != reflect.String {
			return fmt.Errorf(
				"scany: invalid type %v: map must have string key, got: %v",
				dstType, dstType.Key(),
			)
		}
		rs.mapElementType = dstType.Elem()
		rs.scanFn = rs.scanMap
		return nil
	}

	if len(rs.columns) == 1 {
		rs.scanFn = rs.scanPrimitive
		return nil
	}
	return fmt.Errorf(
		"scany: to scan into a primitive type, columns number must be exactly 1, got: %d",
		len(rs.columns),
	)
}

type noOpScanType struct{}

func (*noOpScanType) Scan(value interface{}) error {
	return nil
}

func (rs *RowScanner) scanStruct(structValue reflect.Value) error {
	if rs.scans == nil {
		rs.scans = make([]interface{}, len(rs.columns))
		for _, column := range rs.ptrStructColumns {
			rs.scans[column.pos] = column.value.Addr().Interface()
		}
	}
	for i, column := range rs.columns {
		field, ok := rs.columnToFieldIndex[column]
		if !ok {
			if rs.api.allowUnknownColumns {
				var tmp noOpScanType
				rs.scans[i] = &tmp
				continue
			}
			return fmt.Errorf(
				"scany: column: '%s': no corresponding field found, or it's unexported in %v",
				column, structValue.Type(),
			)
		}
		if field.parent != nil && rs.api.nilStructPointers {
			// The column is in rs.ptrStructColumns, it's scanned into a temporary value.
			continue
		}
		// Struct may contain embedded structs by ptr that defaults to nil.
		// In order to scan values into a nested field,
		// we need to initialize all nil structs on its way.
		initializeNested(structValue, field.index)

		fieldVal := structValue.FieldByIndex(field.index)
		rs.scans[i] = fieldVal.Addr().Interface()
	}
	for _, column := range rs.ptrStructColumns {
		// The value might have been set to a field on the previous row,
		// reset it, so the database library doesn't reuse it.
		column.value.Set(reflect.Zero(column.value.Type()))
	}
	if err := rs.rows.Scan(rs.scans...); err != nil {
		return fmt.Errorf("scany: scan row into struct fields: %w", err)
	}
	return rs.setPtrStructs(structValue)
}

// preparePtrStructs prepares columns that belong to structs by pointer to be scanned into temporary values.
func (rs *RowScanner) preparePtrStructs(structType reflect.Type) {
	known := make(map[*ptrStructMeta]*ptrStructScan)
	for i, column := range rs.columns {
		field, ok := rs.columnToFieldIndex[column]
		if !ok || field.parent == nil {
			continue
		}
		fieldType := structType.FieldByIndex(field.index).Type
		fieldByPtr := fieldType.Kind() == reflect.Ptr
		valueType := fieldType
		if !fieldByPtr {
			valueType = reflect.PtrTo(fieldType)
		}
		rs.ptrStructColumns = append(rs.ptrStructColumns, ptrStructColumn{
			name:       column,
			pos:        i,
			index:      field.index,
			parent:     rs.addPtrStruct(field.parent, known),
			value:      reflect.New(valueType).Elem(),
			fieldByPtr: fieldByPtr,
		})
	}
}

func (rs *RowScanner) addPtrStruct(meta *ptrStructMeta, known map[*ptrStructMeta]*ptrStructScan) *ptrStructScan {
	if ptrStruct, ok := known[meta]; ok {
		return ptrStruct
	}
	ptrStruct := &ptrStructScan{index: meta.index}
	if meta.parent != nil {
		ptrStruct.parent = rs.addPtrStruct(meta.parent, known)
	}
	known[meta] = ptrStruct
	// Parent struct is always added before its nested structs.
	rs.ptrStructs = append(rs.ptrStructs, ptrStruct)
	return ptrStruct
}

// setPtrStructs sets structs by pointer to nil if all their columns are NULL,
// otherwise it initializes them and sets their fields from the scanned temporary values.
func (rs *RowScanner) setPtrStructs(structValue reflect.Value) error {
	rs.markNotNullPtrStructs()
	// Parent structs go first, so all structs on the way to the field are already initialized.
	for _, ptrStruct := range rs.ptrStructs {
		if ptrStruct.parent != nil && !ptrStruct.parent.notNull {
			continue
		}
		field := structValue.FieldByIndex(ptrStruct.index)
		if !ptrStruct.notNull {
			field.Set(reflect.Zero(field.Type()))
		} else if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
	}
	for _, column := range rs.ptrStructColumns {
		if !column.parent.notNull {
			continue
		}
		field, err := structValue.FieldByIndexErr(column.index)
		if err != nil {
			// A column mapped to the struct by pointer itself has set it to nil,
			// columns mapped to its fields are ignored then.
			continue
		}
		switch {
		case column.fieldByPtr:
			field.Set(column.value)
		case !column.value.IsNil():
			field.Set(column.value.Elem())
		default:
			if err := column.setNULL(structValue, field); err != nil {
				return err
			}
		}
	}
	return nil
}

// markNotNullPtrStructs marks structs by pointer that have at least one column that isn't NULL in the current row.
// A column marks its struct and all parent structs.
func (rs *RowScanner) markNotNullPtrStructs() {
	for _, ptrStruct := range rs.ptrStructs {
		ptrStruct.notNull = false
	}
	for _, column := range rs.ptrStructColumns {
		if column.value.IsNil() {
			continue
		}
		for ptrStruct := column.parent; ptrStruct != nil && !ptrStruct.notNull; ptrStruct = ptrStruct.parent {
			ptrStruct.notNull = true
		}
	}
}

// setNULL sets NULL into a field that isn't a pointer, the same way as database libraries do:
// sql.Scanner handles NULL itself, interfaces, maps and slices become nil, other types can't hold NULL.
func (column *ptrStructColumn) setNULL(structValue, field reflect.Value) error {
	if scanner, ok := field.Addr().Interface().(sql.Scanner); ok {
		if err := scanner.Scan(nil); err != nil {
			return fmt.Errorf("scany: column: '%s': scan NULL into %v: %w", column.name, field.Type(), err)
		}
		return nil
	}
	switch field.Kind() {
	case reflect.Interface, reflect.Map, reflect.Slice:
		field.Set(reflect.Zero(field.Type()))
		return nil
	default:
		return fmt.Errorf(
			"scany: column: '%s': cannot scan NULL into %v: %v struct isn't nil because not all of its columns are NULL",
			column.name, field.Type(), structValue.FieldByIndex(column.parent.index).Type(),
		)
	}
}

func (rs *RowScanner) scanMap(mapValue reflect.Value) error {
	if mapValue.IsNil() {
		mapValue.Set(reflect.MakeMap(mapValue.Type()))
	}

	if rs.scans == nil {
		rs.scans = make([]interface{}, len(rs.columns))
	}
	values := make([]reflect.Value, len(rs.columns))
	for i := range rs.columns {
		valuePtr := reflect.New(rs.mapElementType)
		rs.scans[i] = valuePtr.Interface()
		values[i] = valuePtr.Elem()
	}
	if err := rs.rows.Scan(rs.scans...); err != nil {
		return fmt.Errorf("scany: scan rows into map: %w", err)
	}
	// We can't set reflect values into destination map before scanning them,
	// because reflect will set a copy, just like regular map behaves,
	// and scan won't modify the map element.
	for i, column := range rs.columns {
		key := reflect.ValueOf(column)
		value := values[i]
		mapValue.SetMapIndex(key, value)
	}
	return nil
}

func (rs *RowScanner) scanPrimitive(value reflect.Value) error {
	if rs.scans == nil {
		rs.scans = make([]interface{}, 1)
	}
	rs.scans[0] = value.Addr().Interface()
	if err := rs.rows.Scan(rs.scans...); err != nil {
		return fmt.Errorf("scany: scan row value into a primitive type: %w", err)
	}
	return nil
}

func (rs *RowScanner) ensureDistinctColumns() error {
	seen := make(map[string]struct{}, len(rs.columns))
	for _, column := range rs.columns {
		if _, ok := seen[column]; ok {
			return fmt.Errorf("scany: rows contain a duplicate column '%s'", column)
		}
		seen[column] = struct{}{}
	}
	return nil
}
