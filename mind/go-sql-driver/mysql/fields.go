// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2017 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import (
	"database/sql"
	"mind/go-sql-driver"
	"reflect"
)

func (mf *mysqlField) typeDatabaseName() string {
	switch mf.fieldType {
	case go_sql_driver.fieldTypeBit:
		return "BIT"
	case go_sql_driver.fieldTypeBLOB:
		if mf.charSet != go_sql_driver.collations[go_sql_driver.binaryCollation] {
			return "TEXT"
		}
		return "BLOB"
	case go_sql_driver.fieldTypeDate:
		return "DATE"
	case go_sql_driver.fieldTypeDateTime:
		return "DATETIME"
	case go_sql_driver.fieldTypeDecimal:
		return "DECIMAL"
	case go_sql_driver.fieldTypeDouble:
		return "DOUBLE"
	case go_sql_driver.fieldTypeEnum:
		return "ENUM"
	case go_sql_driver.fieldTypeFloat:
		return "FLOAT"
	case go_sql_driver.fieldTypeGeometry:
		return "GEOMETRY"
	case go_sql_driver.fieldTypeInt24:
		return "MEDIUMINT"
	case go_sql_driver.fieldTypeJSON:
		return "JSON"
	case go_sql_driver.fieldTypeLong:
		return "INT"
	case go_sql_driver.fieldTypeLongBLOB:
		if mf.charSet != go_sql_driver.collations[go_sql_driver.binaryCollation] {
			return "LONGTEXT"
		}
		return "LONGBLOB"
	case go_sql_driver.fieldTypeLongLong:
		return "BIGINT"
	case go_sql_driver.fieldTypeMediumBLOB:
		if mf.charSet != go_sql_driver.collations[go_sql_driver.binaryCollation] {
			return "MEDIUMTEXT"
		}
		return "MEDIUMBLOB"
	case go_sql_driver.fieldTypeNewDate:
		return "DATE"
	case go_sql_driver.fieldTypeNewDecimal:
		return "DECIMAL"
	case go_sql_driver.fieldTypeNULL:
		return "NULL"
	case go_sql_driver.fieldTypeSet:
		return "SET"
	case go_sql_driver.fieldTypeShort:
		return "SMALLINT"
	case go_sql_driver.fieldTypeString:
		if mf.charSet == go_sql_driver.collations[go_sql_driver.binaryCollation] {
			return "BINARY"
		}
		return "CHAR"
	case go_sql_driver.fieldTypeTime:
		return "TIME"
	case go_sql_driver.fieldTypeTimestamp:
		return "TIMESTAMP"
	case go_sql_driver.fieldTypeTiny:
		return "TINYINT"
	case go_sql_driver.fieldTypeTinyBLOB:
		if mf.charSet != go_sql_driver.collations[go_sql_driver.binaryCollation] {
			return "TINYTEXT"
		}
		return "TINYBLOB"
	case go_sql_driver.fieldTypeVarChar:
		if mf.charSet == go_sql_driver.collations[go_sql_driver.binaryCollation] {
			return "VARBINARY"
		}
		return "VARCHAR"
	case go_sql_driver.fieldTypeVarString:
		if mf.charSet == go_sql_driver.collations[go_sql_driver.binaryCollation] {
			return "VARBINARY"
		}
		return "VARCHAR"
	case go_sql_driver.fieldTypeYear:
		return "YEAR"
	default:
		return ""
	}
}

var (
	scanTypeFloat32   = reflect.TypeOf(float32(0))
	scanTypeFloat64   = reflect.TypeOf(float64(0))
	scanTypeInt8      = reflect.TypeOf(int8(0))
	scanTypeInt16     = reflect.TypeOf(int16(0))
	scanTypeInt32     = reflect.TypeOf(int32(0))
	scanTypeInt64     = reflect.TypeOf(int64(0))
	scanTypeNullFloat = reflect.TypeOf(sql.NullFloat64{})
	scanTypeNullInt   = reflect.TypeOf(sql.NullInt64{})
	scanTypeNullTime  = reflect.TypeOf(go_sql_driver.NullTime{})
	scanTypeUint8     = reflect.TypeOf(uint8(0))
	scanTypeUint16    = reflect.TypeOf(uint16(0))
	scanTypeUint32    = reflect.TypeOf(uint32(0))
	scanTypeUint64    = reflect.TypeOf(uint64(0))
	scanTypeRawBytes  = reflect.TypeOf(sql.RawBytes{})
	scanTypeUnknown   = reflect.TypeOf(new(interface{}))
)

type mysqlField struct {
	tableName string
	name      string
	length    uint32
	flags     go_sql_driver.fieldFlag
	fieldType go_sql_driver.fieldType
	decimals  byte
	charSet   uint8
}

func (mf *mysqlField) scanType() reflect.Type {
	switch mf.fieldType {
	case go_sql_driver.fieldTypeTiny:
		if mf.flags&go_sql_driver.flagNotNULL != 0 {
			if mf.flags&go_sql_driver.flagUnsigned != 0 {
				return scanTypeUint8
			}
			return scanTypeInt8
		}
		return scanTypeNullInt

	case go_sql_driver.fieldTypeShort, go_sql_driver.fieldTypeYear:
		if mf.flags&go_sql_driver.flagNotNULL != 0 {
			if mf.flags&go_sql_driver.flagUnsigned != 0 {
				return scanTypeUint16
			}
			return scanTypeInt16
		}
		return scanTypeNullInt

	case go_sql_driver.fieldTypeInt24, go_sql_driver.fieldTypeLong:
		if mf.flags&go_sql_driver.flagNotNULL != 0 {
			if mf.flags&go_sql_driver.flagUnsigned != 0 {
				return scanTypeUint32
			}
			return scanTypeInt32
		}
		return scanTypeNullInt

	case go_sql_driver.fieldTypeLongLong:
		if mf.flags&go_sql_driver.flagNotNULL != 0 {
			if mf.flags&go_sql_driver.flagUnsigned != 0 {
				return scanTypeUint64
			}
			return scanTypeInt64
		}
		return scanTypeNullInt

	case go_sql_driver.fieldTypeFloat:
		if mf.flags&go_sql_driver.flagNotNULL != 0 {
			return scanTypeFloat32
		}
		return scanTypeNullFloat

	case go_sql_driver.fieldTypeDouble:
		if mf.flags&go_sql_driver.flagNotNULL != 0 {
			return scanTypeFloat64
		}
		return scanTypeNullFloat

	case go_sql_driver.fieldTypeDecimal, go_sql_driver.fieldTypeNewDecimal, go_sql_driver.fieldTypeVarChar,
		go_sql_driver.fieldTypeBit, go_sql_driver.fieldTypeEnum, go_sql_driver.fieldTypeSet, go_sql_driver.fieldTypeTinyBLOB,
		go_sql_driver.fieldTypeMediumBLOB, go_sql_driver.fieldTypeLongBLOB, go_sql_driver.fieldTypeBLOB,
		go_sql_driver.fieldTypeVarString, go_sql_driver.fieldTypeString, go_sql_driver.fieldTypeGeometry, go_sql_driver.fieldTypeJSON,
		go_sql_driver.fieldTypeTime:
		return scanTypeRawBytes

	case go_sql_driver.fieldTypeDate, go_sql_driver.fieldTypeNewDate,
		go_sql_driver.fieldTypeTimestamp, go_sql_driver.fieldTypeDateTime:
		// NullTime is always returned for more consistent behavior as it can
		// handle both cases of parseTime regardless if the field is nullable.
		return scanTypeNullTime

	default:
		return scanTypeUnknown
	}
}
