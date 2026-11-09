package store

/*
#cgo LDFLAGS: -lsqlite3
#include <stdlib.h>
#include <sqlite3.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"unsafe"
)

type row map[string]any

type database struct{ handle *C.sqlite3 }

func openDatabase(path string) (*database, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var handle *C.sqlite3
	flags := C.int(C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_FULLMUTEX)
	if code := C.sqlite3_open_v2(cpath, &handle, flags, nil); code != C.SQLITE_OK {
		if handle != nil {
			msg := C.GoString(C.sqlite3_errmsg(handle))
			C.sqlite3_close(handle)
			return nil, fmt.Errorf("open sqlite: %s", msg)
		}
		return nil, errors.New("open sqlite failed")
	}
	db := &database{handle}
	C.sqlite3_busy_timeout(handle, 10000)
	for _, statement := range []string{"PRAGMA foreign_keys = ON", "PRAGMA journal_mode = WAL"} {
		if err := db.run(statement); err != nil {
			db.close()
			return nil, err
		}
	}
	return db, nil
}
func (db *database) close() error {
	if db.handle == nil {
		return nil
	}
	code := C.sqlite3_close(db.handle)
	if code != C.SQLITE_OK {
		return db.err()
	}
	db.handle = nil
	return nil
}
func (db *database) err() error { return errors.New(C.GoString(C.sqlite3_errmsg(db.handle))) }
func (db *database) script(sql string) error {
	cs := C.CString(sql)
	defer C.free(unsafe.Pointer(cs))
	var errmsg *C.char
	if C.sqlite3_exec(db.handle, cs, nil, nil, &errmsg) != C.SQLITE_OK {
		msg := C.GoString(errmsg)
		C.sqlite3_free(unsafe.Pointer(errmsg))
		return errors.New(msg)
	}
	return nil
}
func (db *database) prepare(query string, args []any) (*C.sqlite3_stmt, error) {
	cq := C.CString(query)
	defer C.free(unsafe.Pointer(cq))
	var stmt *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(db.handle, cq, -1, &stmt, nil) != C.SQLITE_OK {
		return nil, db.err()
	}
	if C.sqlite3_bind_parameter_count(stmt) != C.int(len(args)) {
		C.sqlite3_finalize(stmt)
		return nil, fmt.Errorf("incorrect argument count in SQL statement")
	}
	for idx, value := range args {
		i := C.int(idx + 1)
		var code C.int
		switch v := value.(type) {
		case nil:
			code = C.sqlite3_bind_null(stmt, i)
		case bool:
			if v {
				code = C.sqlite3_bind_int64(stmt, i, 1)
			} else {
				code = C.sqlite3_bind_int64(stmt, i, 0)
			}
		case int:
			code = C.sqlite3_bind_int64(stmt, i, C.sqlite3_int64(v))
		case int64:
			code = C.sqlite3_bind_int64(stmt, i, C.sqlite3_int64(v))
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				C.sqlite3_finalize(stmt)
				return nil, errors.New("invalid SQL float")
			}
			code = C.sqlite3_bind_double(stmt, i, C.double(v))
		case string:
			cs := C.CString(v)
			code = C.sqlite3_bind_text(stmt, i, cs, C.int(len(v)), C.SQLITE_TRANSIENT)
			C.free(unsafe.Pointer(cs))
		case []byte:
			data := C.CBytes(v)
			code = C.sqlite3_bind_blob(stmt, i, data, C.int(len(v)), C.SQLITE_TRANSIENT)
			C.free(data)
		default:
			C.sqlite3_finalize(stmt)
			return nil, fmt.Errorf("unsupported SQL type %T", value)
		}
		if code != C.SQLITE_OK {
			C.sqlite3_finalize(stmt)
			return nil, db.err()
		}
	}
	return stmt, nil
}
func (db *database) run(query string, args ...any) error {
	stmt, err := db.prepare(query, args)
	if err != nil {
		return err
	}
	defer C.sqlite3_finalize(stmt)
	for {
		code := C.sqlite3_step(stmt)
		if code == C.SQLITE_DONE {
			return nil
		}
		if code != C.SQLITE_ROW {
			return db.err()
		}
	}
}
func (db *database) rows(query string, args ...any) ([]row, error) {
	stmt, err := db.prepare(query, args)
	if err != nil {
		return nil, err
	}
	defer C.sqlite3_finalize(stmt)
	result := []row{}
	for {
		code := C.sqlite3_step(stmt)
		if code == C.SQLITE_DONE {
			return result, nil
		}
		if code != C.SQLITE_ROW {
			return nil, db.err()
		}
		item := row{}
		cols := int(C.sqlite3_column_count(stmt))
		for i := 0; i < cols; i++ {
			name := C.GoString(C.sqlite3_column_name(stmt, C.int(i)))
			switch C.sqlite3_column_type(stmt, C.int(i)) {
			case C.SQLITE_NULL:
				item[name] = nil
			case C.SQLITE_INTEGER:
				item[name] = int64(C.sqlite3_column_int64(stmt, C.int(i)))
			case C.SQLITE_FLOAT:
				item[name] = float64(C.sqlite3_column_double(stmt, C.int(i)))
			case C.SQLITE_TEXT:
				ptr := C.sqlite3_column_text(stmt, C.int(i))
				length := C.sqlite3_column_bytes(stmt, C.int(i))
				item[name] = C.GoStringN((*C.char)(unsafe.Pointer(ptr)), length)
			case C.SQLITE_BLOB:
				ptr := C.sqlite3_column_blob(stmt, C.int(i))
				length := C.sqlite3_column_bytes(stmt, C.int(i))
				item[name] = C.GoBytes(ptr, length)
			default:
				return nil, errors.New("unrecognized SQLite type")
			}
		}
		result = append(result, item)
	}
}
func (db *database) one(query string, args ...any) (row, error) {
	rows, err := db.rows(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}
func (db *database) count(query string, args ...any) (int, error) {
	r, e := db.one(query, args...)
	if e != nil {
		return 0, e
	}
	for _, v := range r {
		return intVal(v), nil
	}
	return 0, nil
}
func intVal(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
func strVal(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func boolVal(v any) bool { return intVal(v) != 0 }
func (db *database) require(query string, args ...any) (row, error) {
	r, e := db.one(query, args...)
	if e != nil {
		return nil, e
	}
	if r == nil {
		return nil, errors.New("record not found")
	}
	return r, nil
}
