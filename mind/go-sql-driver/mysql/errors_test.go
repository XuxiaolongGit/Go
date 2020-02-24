// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2013 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import (
	"bytes"
	"log"
	"mind/go-sql-driver"
	"testing"
)

func TestErrorsSetLogger(t *testing.T) {
	previous := go_sql_driver.errLog
	defer func() {
		go_sql_driver.errLog = previous
	}()

	// set up logger
	const expected = "prefix: test\n"
	buffer := bytes.NewBuffer(make([]byte, 0, 64))
	logger := log.New(buffer, "prefix: ", 0)

	// print
	go_sql_driver.SetLogger(logger)
	go_sql_driver.errLog.Print("test")

	// check result
	if actual := buffer.String(); actual != expected {
		t.Errorf("expected %q, got %q", expected, actual)
	}
}

func TestErrorsStrictIgnoreNotes(t *testing.T) {
	go_sql_driver.runTests(t, go_sql_driver.dsn+"&sql_notes=false", func(dbt *go_sql_driver.DBTest) {
		dbt.mustExec("DROP TABLE IF EXISTS does_not_exist")
	})
}
