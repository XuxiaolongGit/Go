// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2012 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import (
	"bytes"
	"crypto/tls"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"mind/go-sql-driver"
	"time"
)

// Packets documentation:
// http://dev.mysql.com/doc/internals/en/client-server-protocol.html

// Read packet to buffer 'data'
func (mc *go_sql_driver.mysqlConn) readPacket() ([]byte, error) {
	var prevData []byte
	for {
		// read packet header
		data, err := mc.buf.readNext(4)
		if err != nil {
			if cerr := mc.canceled.Value(); cerr != nil {
				return nil, cerr
			}
			go_sql_driver.errLog.Print(err)
			mc.Close()
			return nil, go_sql_driver.ErrInvalidConn
		}

		// packet length [24 bit]
		pktLen := int(uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16)

		// check packet sync [8 bit]
		if data[3] != mc.sequence {
			if data[3] > mc.sequence {
				return nil, go_sql_driver.ErrPktSyncMul
			}
			return nil, go_sql_driver.ErrPktSync
		}
		mc.sequence++

		// packets with length 0 terminate a previous packet which is a
		// multiple of (2^24)−1 bytes long
		if pktLen == 0 {
			// there was no previous packet
			if prevData == nil {
				go_sql_driver.errLog.Print(go_sql_driver.ErrMalformPkt)
				mc.Close()
				return nil, go_sql_driver.ErrInvalidConn
			}

			return prevData, nil
		}

		// read packet body [pktLen bytes]
		data, err = mc.buf.readNext(pktLen)
		if err != nil {
			if cerr := mc.canceled.Value(); cerr != nil {
				return nil, cerr
			}
			go_sql_driver.errLog.Print(err)
			mc.Close()
			return nil, go_sql_driver.ErrInvalidConn
		}

		// return data if this was the last packet
		if pktLen < go_sql_driver.maxPacketSize {
			// zero allocations for non-split packets
			if prevData == nil {
				return data, nil
			}

			return append(prevData, data...), nil
		}

		prevData = append(prevData, data...)
	}
}

// Write packet buffer 'data'
func (mc *go_sql_driver.mysqlConn) writePacket(data []byte) error {
	pktLen := len(data) - 4

	if pktLen > mc.maxAllowedPacket {
		return go_sql_driver.ErrPktTooLarge
	}

	for {
		var size int
		if pktLen >= go_sql_driver.maxPacketSize {
			data[0] = 0xff
			data[1] = 0xff
			data[2] = 0xff
			size = go_sql_driver.maxPacketSize
		} else {
			data[0] = byte(pktLen)
			data[1] = byte(pktLen >> 8)
			data[2] = byte(pktLen >> 16)
			size = pktLen
		}
		data[3] = mc.sequence

		// Write packet
		if mc.writeTimeout > 0 {
			if err := mc.netConn.SetWriteDeadline(time.Now().Add(mc.writeTimeout)); err != nil {
				return err
			}
		}

		n, err := mc.netConn.Write(data[:4+size])
		if err == nil && n == 4+size {
			mc.sequence++
			if size != go_sql_driver.maxPacketSize {
				return nil
			}
			pktLen -= size
			data = data[size:]
			continue
		}

		// Handle error
		if err == nil { // n != len(data)
			mc.cleanup()
			go_sql_driver.errLog.Print(go_sql_driver.ErrMalformPkt)
		} else {
			if cerr := mc.canceled.Value(); cerr != nil {
				return cerr
			}
			if n == 0 && pktLen == len(data)-4 {
				// only for the first loop iteration when nothing was written yet
				return go_sql_driver.errBadConnNoWrite
			}
			mc.cleanup()
			go_sql_driver.errLog.Print(err)
		}
		return go_sql_driver.ErrInvalidConn
	}
}

/******************************************************************************
*                           Initialization Process                            *
******************************************************************************/

// Handshake Initialization Packet
// http://dev.mysql.com/doc/internals/en/connection-phase-packets.html#packet-Protocol::Handshake
func (mc *go_sql_driver.mysqlConn) readHandshakePacket() (data []byte, plugin string, err error) {
	data, err = mc.readPacket()
	if err != nil {
		// for init we can rewrite this to ErrBadConn for sql.Driver to retry, since
		// in connection initialization we don't risk retrying non-idempotent actions.
		if err == go_sql_driver.ErrInvalidConn {
			return nil, "", driver.ErrBadConn
		}
		return
	}

	if data[0] == go_sql_driver.iERR {
		return nil, "", mc.handleErrorPacket(data)
	}

	// protocol version [1 byte]
	if data[0] < go_sql_driver.minProtocolVersion {
		return nil, "", fmt.Errorf(
			"unsupported protocol version %d. Version %d or higher is required",
			data[0],
			go_sql_driver.minProtocolVersion,
		)
	}

	// server version [null terminated string]
	// connection id [4 bytes]
	pos := 1 + bytes.IndexByte(data[1:], 0x00) + 1 + 4

	// first part of the password cipher [8 bytes]
	authData := data[pos : pos+8]

	// (filler) always 0x00 [1 byte]
	pos += 8 + 1

	// capability flags (lower 2 bytes) [2 bytes]
	mc.flags = go_sql_driver.clientFlag(binary.LittleEndian.Uint16(data[pos : pos+2]))
	if mc.flags&go_sql_driver.clientProtocol41 == 0 {
		return nil, "", go_sql_driver.ErrOldProtocol
	}
	if mc.flags&go_sql_driver.clientSSL == 0 && mc.cfg.tls != nil {
		return nil, "", go_sql_driver.ErrNoTLS
	}
	pos += 2

	if len(data) > pos {
		// character set [1 byte]
		// status flags [2 bytes]
		// capability flags (upper 2 bytes) [2 bytes]
		// length of auth-plugin-data [1 byte]
		// reserved (all [00]) [10 bytes]
		pos += 1 + 2 + 2 + 1 + 10

		// second part of the password cipher [mininum 13 bytes],
		// where len=MAX(13, length of auth-plugin-data - 8)
		//
		// The web documentation is ambiguous about the length. However,
		// according to mysql-5.7/sql/auth/sql_authentication.cc line 538,
		// the 13th byte is "\0 byte, terminating the second part of
		// a scramble". So the second part of the password cipher is
		// a NULL terminated string that's at least 13 bytes with the
		// last byte being NULL.
		//
		// The official Python library uses the fixed length 12
		// which seems to work but technically could have a hidden bug.
		authData = append(authData, data[pos:pos+12]...)
		pos += 13

		// EOF if version (>= 5.5.7 and < 5.5.10) or (>= 5.6.0 and < 5.6.2)
		// \NUL otherwise
		if end := bytes.IndexByte(data[pos:], 0x00); end != -1 {
			plugin = string(data[pos : pos+end])
		} else {
			plugin = string(data[pos:])
		}

		// make a memory safe copy of the cipher slice
		var b [20]byte
		copy(b[:], authData)
		return b[:], plugin, nil
	}

	// make a memory safe copy of the cipher slice
	var b [8]byte
	copy(b[:], authData)
	return b[:], plugin, nil
}

// Client Authentication Packet
// http://dev.mysql.com/doc/internals/en/connection-phase-packets.html#packet-Protocol::HandshakeResponse
func (mc *go_sql_driver.mysqlConn) writeHandshakeResponsePacket(authResp []byte, addNUL bool, plugin string) error {
	// Adjust client flags based on server support
	clientFlags := go_sql_driver.clientProtocol41 |
		go_sql_driver.clientSecureConn |
		go_sql_driver.clientLongPassword |
		go_sql_driver.clientTransactions |
		go_sql_driver.clientLocalFiles |
		go_sql_driver.clientPluginAuth |
		go_sql_driver.clientMultiResults |
		mc.flags&go_sql_driver.clientLongFlag

	if mc.cfg.ClientFoundRows {
		clientFlags |= go_sql_driver.clientFoundRows
	}

	// To enable TLS / SSL
	if mc.cfg.tls != nil {
		clientFlags |= go_sql_driver.clientSSL
	}

	if mc.cfg.MultiStatements {
		clientFlags |= go_sql_driver.clientMultiStatements
	}

	// encode length of the auth plugin data
	var authRespLEIBuf [9]byte
	authRespLEI := go_sql_driver.appendLengthEncodedInteger(authRespLEIBuf[:0], uint64(len(authResp)))
	if len(authRespLEI) > 1 {
		// if the length can not be written in 1 byte, it must be written as a
		// length encoded integer
		clientFlags |= go_sql_driver.clientPluginAuthLenEncClientData
	}

	pktLen := 4 + 4 + 1 + 23 + len(mc.cfg.User) + 1 + len(authRespLEI) + len(authResp) + 21 + 1
	if addNUL {
		pktLen++
	}

	// To specify a db name
	if n := len(mc.cfg.DBName); n > 0 {
		clientFlags |= go_sql_driver.clientConnectWithDB
		pktLen += n + 1
	}

	// Calculate packet length and get buffer with that size
	data := mc.buf.takeSmallBuffer(pktLen + 4)
	if data == nil {
		// cannot take the buffer. Something must be wrong with the connection
		go_sql_driver.errLog.Print(go_sql_driver.ErrBusyBuffer)
		return go_sql_driver.errBadConnNoWrite
	}

	// ClientFlags [32 bit]
	data[4] = byte(clientFlags)
	data[5] = byte(clientFlags >> 8)
	data[6] = byte(clientFlags >> 16)
	data[7] = byte(clientFlags >> 24)

	// MaxPacketSize [32 bit] (none)
	data[8] = 0x00
	data[9] = 0x00
	data[10] = 0x00
	data[11] = 0x00

	// Charset [1 byte]
	var found bool
	data[12], found = go_sql_driver.collations[mc.cfg.Collation]
	if !found {
		// Note possibility for false negatives:
		// could be triggered  although the collation is valid if the
		// collations map does not contain entries the server supports.
		return errors.New("unknown collation")
	}

	// SSL Connection Request Packet
	// http://dev.mysql.com/doc/internals/en/connection-phase-packets.html#packet-Protocol::SSLRequest
	if mc.cfg.tls != nil {
		// Send TLS / SSL request packet
		if err := mc.writePacket(data[:(4+4+1+23)+4]); err != nil {
			return err
		}

		// Switch to TLS
		tlsConn := tls.Client(mc.netConn, mc.cfg.tls)
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		mc.netConn = tlsConn
		mc.buf.nc = tlsConn
	}

	// Filler [23 bytes] (all 0x00)
	pos := 13
	for ; pos < 13+23; pos++ {
		data[pos] = 0
	}

	// User [null terminated string]
	if len(mc.cfg.User) > 0 {
		pos += copy(data[pos:], mc.cfg.User)
	}
	data[pos] = 0x00
	pos++

	// Auth Data [length encoded integer]
	pos += copy(data[pos:], authRespLEI)
	pos += copy(data[pos:], authResp)
	if addNUL {
		data[pos] = 0x00
		pos++
	}

	// Databasename [null terminated string]
	if len(mc.cfg.DBName) > 0 {
		pos += copy(data[pos:], mc.cfg.DBName)
		data[pos] = 0x00
		pos++
	}

	pos += copy(data[pos:], plugin)
	data[pos] = 0x00

	// Send Auth packet
	return mc.writePacket(data)
}

// http://dev.mysql.com/doc/internals/en/connection-phase-packets.html#packet-Protocol::AuthSwitchResponse
func (mc *go_sql_driver.mysqlConn) writeAuthSwitchPacket(authData []byte, addNUL bool) error {
	pktLen := 4 + len(authData)
	if addNUL {
		pktLen++
	}
	data := mc.buf.takeSmallBuffer(pktLen)
	if data == nil {
		// cannot take the buffer. Something must be wrong with the connection
		go_sql_driver.errLog.Print(go_sql_driver.ErrBusyBuffer)
		return go_sql_driver.errBadConnNoWrite
	}

	// Add the auth data [EOF]
	copy(data[4:], authData)
	if addNUL {
		data[pktLen-1] = 0x00
	}

	return mc.writePacket(data)
}

/******************************************************************************
*                             Command Packets                                 *
******************************************************************************/

func (mc *go_sql_driver.mysqlConn) writeCommandPacket(command byte) error {
	// Reset Packet Sequence
	mc.sequence = 0

	data := mc.buf.takeSmallBuffer(4 + 1)
	if data == nil {
		// cannot take the buffer. Something must be wrong with the connection
		go_sql_driver.errLog.Print(go_sql_driver.ErrBusyBuffer)
		return go_sql_driver.errBadConnNoWrite
	}

	// Add command byte
	data[4] = command

	// Send CMD packet
	return mc.writePacket(data)
}

func (mc *go_sql_driver.mysqlConn) writeCommandPacketStr(command byte, arg string) error {
	// Reset Packet Sequence
	mc.sequence = 0

	pktLen := 1 + len(arg)
	data := mc.buf.takeBuffer(pktLen + 4)
	if data == nil {
		// cannot take the buffer. Something must be wrong with the connection
		go_sql_driver.errLog.Print(go_sql_driver.ErrBusyBuffer)
		return go_sql_driver.errBadConnNoWrite
	}

	// Add command byte
	data[4] = command

	// Add arg
	copy(data[5:], arg)

	// Send CMD packet
	return mc.writePacket(data)
}

func (mc *go_sql_driver.mysqlConn) writeCommandPacketUint32(command byte, arg uint32) error {
	// Reset Packet Sequence
	mc.sequence = 0

	data := mc.buf.takeSmallBuffer(4 + 1 + 4)
	if data == nil {
		// cannot take the buffer. Something must be wrong with the connection
		go_sql_driver.errLog.Print(go_sql_driver.ErrBusyBuffer)
		return go_sql_driver.errBadConnNoWrite
	}

	// Add command byte
	data[4] = command

	// Add arg [32 bit]
	data[5] = byte(arg)
	data[6] = byte(arg >> 8)
	data[7] = byte(arg >> 16)
	data[8] = byte(arg >> 24)

	// Send CMD packet
	return mc.writePacket(data)
}

/******************************************************************************
*                              Result Packets                                 *
******************************************************************************/

func (mc *go_sql_driver.mysqlConn) readAuthResult() ([]byte, string, error) {
	data, err := mc.readPacket()
	if err != nil {
		return nil, "", err
	}

	// packet indicator
	switch data[0] {

	case go_sql_driver.iOK:
		return nil, "", mc.handleOkPacket(data)

	case go_sql_driver.iAuthMoreData:
		return data[1:], "", err

	case go_sql_driver.iEOF:
		if len(data) < 1 {
			// https://dev.mysql.com/doc/internals/en/connection-phase-packets.html#packet-Protocol::OldAuthSwitchRequest
			return nil, "mysql_old_password", nil
		}
		pluginEndIndex := bytes.IndexByte(data, 0x00)
		if pluginEndIndex < 0 {
			return nil, "", go_sql_driver.ErrMalformPkt
		}
		plugin := string(data[1:pluginEndIndex])
		authData := data[pluginEndIndex+1:]
		return authData, plugin, nil

	default: // Error otherwise
		return nil, "", mc.handleErrorPacket(data)
	}
}

// Returns error if Packet is not an 'Result OK'-Packet
func (mc *go_sql_driver.mysqlConn) readResultOK() error {
	data, err := mc.readPacket()
	if err != nil {
		return err
	}

	if data[0] == go_sql_driver.iOK {
		return mc.handleOkPacket(data)
	}
	return mc.handleErrorPacket(data)
}

// Result Set Header Packet
// http://dev.mysql.com/doc/internals/en/com-query-response.html#packet-ProtocolText::Resultset
func (mc *go_sql_driver.mysqlConn) readResultSetHeaderPacket() (int, error) {
	data, err := mc.readPacket()
	if err == nil {
		switch data[0] {

		case go_sql_driver.iOK:
			return 0, mc.handleOkPacket(data)

		case go_sql_driver.iERR:
			return 0, mc.handleErrorPacket(data)

		case go_sql_driver.iLocalInFile:
			return 0, mc.handleInFileRequest(string(data[1:]))
		}

		// column count
		num, _, n := go_sql_driver.readLengthEncodedInteger(data)
		if n-len(data) == 0 {
			return int(num), nil
		}

		return 0, go_sql_driver.ErrMalformPkt
	}
	return 0, err
}

// Error Packet
// http://dev.mysql.com/doc/internals/en/generic-response-packets.html#packet-ERR_Packet
func (mc *go_sql_driver.mysqlConn) handleErrorPacket(data []byte) error {
	if data[0] != go_sql_driver.iERR {
		return go_sql_driver.ErrMalformPkt
	}

	// 0xff [1 byte]

	// Error Number [16 bit uint]
	errno := binary.LittleEndian.Uint16(data[1:3])

	// 1792: ER_CANT_EXECUTE_IN_READ_ONLY_TRANSACTION
	// 1290: ER_OPTION_PREVENTS_STATEMENT (returned by Aurora during failover)
	if (errno == 1792 || errno == 1290) && mc.cfg.RejectReadOnly {
		// Oops; we are connected to a read-only connection, and won't be able
		// to issue any write statements. Since RejectReadOnly is configured,
		// we throw away this connection hoping this one would have write
		// permission. This is specifically for a possible race condition
		// during failover (e.g. on AWS Aurora). See README.md for more.
		//
		// We explicitly close the connection before returning
		// driver.ErrBadConn to ensure that `database/sql` purges this
		// connection and initiates a new one for next statement next time.
		mc.Close()
		return driver.ErrBadConn
	}

	pos := 3

	// SQL State [optional: # + 5bytes string]
	if data[3] == 0x23 {
		//sqlstate := string(data[4 : 4+5])
		pos = 9
	}

	// Error Message [string]
	return &go_sql_driver.MySQLError{
		Number:  errno,
		Message: string(data[pos:]),
	}
}

func readStatus(b []byte) go_sql_driver.statusFlag {
	return go_sql_driver.statusFlag(b[0]) | go_sql_driver.statusFlag(b[1])<<8
}

// Ok Packet
// http://dev.mysql.com/doc/internals/en/generic-response-packets.html#packet-OK_Packet
func (mc *go_sql_driver.mysqlConn) handleOkPacket(data []byte) error {
	var n, m int

	// 0x00 [1 byte]

	// Affected rows [Length Coded Binary]
	mc.affectedRows, _, n = go_sql_driver.readLengthEncodedInteger(data[1:])

	// Insert id [Length Coded Binary]
	mc.insertId, _, m = go_sql_driver.readLengthEncodedInteger(data[1+n:])

	// server_status [2 bytes]
	mc.status = readStatus(data[1+n+m : 1+n+m+2])
	if mc.status&go_sql_driver.statusMoreResultsExists != 0 {
		return nil
	}

	// warning count [2 bytes]

	return nil
}

// Read Packets as Field Packets until EOF-Packet or an Error appears
// http://dev.mysql.com/doc/internals/en/com-query-response.html#packet-Protocol::ColumnDefinition41
func (mc *go_sql_driver.mysqlConn) readColumns(count int) ([]go_sql_driver.mysqlField, error) {
	columns := make([]go_sql_driver.mysqlField, count)

	for i := 0; ; i++ {
		data, err := mc.readPacket()
		if err != nil {
			return nil, err
		}

		// EOF Packet
		if data[0] == go_sql_driver.iEOF && (len(data) == 5 || len(data) == 1) {
			if i == count {
				return columns, nil
			}
			return nil, fmt.Errorf("column count mismatch n:%d len:%d", count, len(columns))
		}

		// Catalog
		pos, err := go_sql_driver.skipLengthEncodedString(data)
		if err != nil {
			return nil, err
		}

		// Database [len coded string]
		n, err := go_sql_driver.skipLengthEncodedString(data[pos:])
		if err != nil {
			return nil, err
		}
		pos += n

		// Table [len coded string]
		if mc.cfg.ColumnsWithAlias {
			tableName, _, n, err := go_sql_driver.readLengthEncodedString(data[pos:])
			if err != nil {
				return nil, err
			}
			pos += n
			columns[i].tableName = string(tableName)
		} else {
			n, err = go_sql_driver.skipLengthEncodedString(data[pos:])
			if err != nil {
				return nil, err
			}
			pos += n
		}

		// Original table [len coded string]
		n, err = go_sql_driver.skipLengthEncodedString(data[pos:])
		if err != nil {
			return nil, err
		}
		pos += n

		// Name [len coded string]
		name, _, n, err := go_sql_driver.readLengthEncodedString(data[pos:])
		if err != nil {
			return nil, err
		}
		columns[i].name = string(name)
		pos += n

		// Original name [len coded string]
		n, err = go_sql_driver.skipLengthEncodedString(data[pos:])
		if err != nil {
			return nil, err
		}
		pos += n

		// Filler [uint8]
		pos++

		// Charset [charset, collation uint8]
		columns[i].charSet = data[pos]
		pos += 2

		// Length [uint32]
		columns[i].length = binary.LittleEndian.Uint32(data[pos : pos+4])
		pos += 4

		// Field type [uint8]
		columns[i].fieldType = go_sql_driver.fieldType(data[pos])
		pos++

		// Flags [uint16]
		columns[i].flags = go_sql_driver.fieldFlag(binary.LittleEndian.Uint16(data[pos : pos+2]))
		pos += 2

		// Decimals [uint8]
		columns[i].decimals = data[pos]
		//pos++

		// Default value [len coded binary]
		//if pos < len(data) {
		//	defaultVal, _, err = bytesToLengthCodedBinary(data[pos:])
		//}
	}
}

// Read Packets as Field Packets until EOF-Packet or an Error appears
// http://dev.mysql.com/doc/internals/en/com-query-response.html#packet-ProtocolText::ResultsetRow
func (rows *go_sql_driver.textRows) readRow(dest []driver.Value) error {
	mc := rows.mc

	if rows.rs.done {
		return io.EOF
	}

	data, err := mc.readPacket()
	if err != nil {
		return err
	}

	// EOF Packet
	if data[0] == go_sql_driver.iEOF && len(data) == 5 {
		// server_status [2 bytes]
		rows.mc.status = readStatus(data[3:])
		rows.rs.done = true
		if !rows.HasNextResultSet() {
			rows.mc = nil
		}
		return io.EOF
	}
	if data[0] == go_sql_driver.iERR {
		rows.mc = nil
		return mc.handleErrorPacket(data)
	}

	// RowSet Packet
	var n int
	var isNull bool
	pos := 0

	for i := range dest {
		// Read bytes and convert to string
		dest[i], isNull, n, err = go_sql_driver.readLengthEncodedString(data[pos:])
		pos += n
		if err == nil {
			if !isNull {
				if !mc.parseTime {
					continue
				} else {
					switch rows.rs.columns[i].fieldType {
					case go_sql_driver.fieldTypeTimestamp, go_sql_driver.fieldTypeDateTime,
						go_sql_driver.fieldTypeDate, go_sql_driver.fieldTypeNewDate:
						dest[i], err = go_sql_driver.parseDateTime(
							string(dest[i].([]byte)),
							mc.cfg.Loc,
						)
						if err == nil {
							continue
						}
					default:
						continue
					}
				}

			} else {
				dest[i] = nil
				continue
			}
		}
		return err // err != nil
	}

	return nil
}

// Reads Packets until EOF-Packet or an Error appears. Returns count of Packets read
func (mc *go_sql_driver.mysqlConn) readUntilEOF() error {
	for {
		data, err := mc.readPacket()
		if err != nil {
			return err
		}

		switch data[0] {
		case go_sql_driver.iERR:
			return mc.handleErrorPacket(data)
		case go_sql_driver.iEOF:
			if len(data) == 5 {
				mc.status = readStatus(data[3:])
			}
			return nil
		}
	}
}

/******************************************************************************
*                           Prepared Statements                               *
******************************************************************************/

// Prepare Result Packets
// http://dev.mysql.com/doc/internals/en/com-stmt-prepare-response.html
func (stmt *go_sql_driver.mysqlStmt) readPrepareResultPacket() (uint16, error) {
	data, err := stmt.mc.readPacket()
	if err == nil {
		// packet indicator [1 byte]
		if data[0] != go_sql_driver.iOK {
			return 0, stmt.mc.handleErrorPacket(data)
		}

		// statement id [4 bytes]
		stmt.id = binary.LittleEndian.Uint32(data[1:5])

		// Column count [16 bit uint]
		columnCount := binary.LittleEndian.Uint16(data[5:7])

		// Param count [16 bit uint]
		stmt.paramCount = int(binary.LittleEndian.Uint16(data[7:9]))

		// Reserved [8 bit]

		// Warning count [16 bit uint]

		return columnCount, nil
	}
	return 0, err
}

// http://dev.mysql.com/doc/internals/en/com-stmt-send-long-data.html
func (stmt *go_sql_driver.mysqlStmt) writeCommandLongData(paramID int, arg []byte) error {
	maxLen := stmt.mc.maxAllowedPacket - 1
	pktLen := maxLen

	// After the header (bytes 0-3) follows before the data:
	// 1 byte command
	// 4 bytes stmtID
	// 2 bytes paramID
	const dataOffset = 1 + 4 + 2

	// Cannot use the write buffer since
	// a) the buffer is too small
	// b) it is in use
	data := make([]byte, 4+1+4+2+len(arg))

	copy(data[4+dataOffset:], arg)

	for argLen := len(arg); argLen > 0; argLen -= pktLen - dataOffset {
		if dataOffset+argLen < maxLen {
			pktLen = dataOffset + argLen
		}

		stmt.mc.sequence = 0
		// Add command byte [1 byte]
		data[4] = go_sql_driver.comStmtSendLongData

		// Add stmtID [32 bit]
		data[5] = byte(stmt.id)
		data[6] = byte(stmt.id >> 8)
		data[7] = byte(stmt.id >> 16)
		data[8] = byte(stmt.id >> 24)

		// Add paramID [16 bit]
		data[9] = byte(paramID)
		data[10] = byte(paramID >> 8)

		// Send CMD packet
		err := stmt.mc.writePacket(data[:4+pktLen])
		if err == nil {
			data = data[pktLen-dataOffset:]
			continue
		}
		return err

	}

	// Reset Packet Sequence
	stmt.mc.sequence = 0
	return nil
}

// Execute Prepared Statement
// http://dev.mysql.com/doc/internals/en/com-stmt-execute.html
func (stmt *go_sql_driver.mysqlStmt) writeExecutePacket(args []driver.Value) error {
	if len(args) != stmt.paramCount {
		return fmt.Errorf(
			"argument count mismatch (got: %d; has: %d)",
			len(args),
			stmt.paramCount,
		)
	}

	const minPktLen = 4 + 1 + 4 + 1 + 4
	mc := stmt.mc

	// Determine threshould dynamically to avoid packet size shortage.
	longDataSize := mc.maxAllowedPacket / (stmt.paramCount + 1)
	if longDataSize < 64 {
		longDataSize = 64
	}

	// Reset packet-sequence
	mc.sequence = 0

	var data []byte

	if len(args) == 0 {
		data = mc.buf.takeBuffer(minPktLen)
	} else {
		data = mc.buf.takeCompleteBuffer()
	}
	if data == nil {
		// cannot take the buffer. Something must be wrong with the connection
		go_sql_driver.errLog.Print(go_sql_driver.ErrBusyBuffer)
		return go_sql_driver.errBadConnNoWrite
	}

	// command [1 byte]
	data[4] = go_sql_driver.comStmtExecute

	// statement_id [4 bytes]
	data[5] = byte(stmt.id)
	data[6] = byte(stmt.id >> 8)
	data[7] = byte(stmt.id >> 16)
	data[8] = byte(stmt.id >> 24)

	// flags (0: CURSOR_TYPE_NO_CURSOR) [1 byte]
	data[9] = 0x00

	// iteration_count (uint32(1)) [4 bytes]
	data[10] = 0x01
	data[11] = 0x00
	data[12] = 0x00
	data[13] = 0x00

	if len(args) > 0 {
		pos := minPktLen

		var nullMask []byte
		if maskLen, typesLen := (len(args)+7)/8, 1+2*len(args); pos+maskLen+typesLen >= len(data) {
			// buffer has to be extended but we don't know by how much so
			// we depend on append after all data with known sizes fit.
			// We stop at that because we deal with a lot of columns here
			// which makes the required allocation size hard to guess.
			tmp := make([]byte, pos+maskLen+typesLen)
			copy(tmp[:pos], data[:pos])
			data = tmp
			nullMask = data[pos : pos+maskLen]
			pos += maskLen
		} else {
			nullMask = data[pos : pos+maskLen]
			for i := 0; i < maskLen; i++ {
				nullMask[i] = 0
			}
			pos += maskLen
		}

		// newParameterBoundFlag 1 [1 byte]
		data[pos] = 0x01
		pos++

		// type of each parameter [len(args)*2 bytes]
		paramTypes := data[pos:]
		pos += len(args) * 2

		// value of each parameter [n bytes]
		paramValues := data[pos:pos]
		valuesCap := cap(paramValues)

		for i, arg := range args {
			// build NULL-bitmap
			if arg == nil {
				nullMask[i/8] |= 1 << (uint(i) & 7)
				paramTypes[i+i] = byte(go_sql_driver.fieldTypeNULL)
				paramTypes[i+i+1] = 0x00
				continue
			}

			// cache types and values
			switch v := arg.(type) {
			case int64:
				paramTypes[i+i] = byte(go_sql_driver.fieldTypeLongLong)
				paramTypes[i+i+1] = 0x00

				if cap(paramValues)-len(paramValues)-8 >= 0 {
					paramValues = paramValues[:len(paramValues)+8]
					binary.LittleEndian.PutUint64(
						paramValues[len(paramValues)-8:],
						uint64(v),
					)
				} else {
					paramValues = append(paramValues,
						go_sql_driver.uint64ToBytes(uint64(v))...,
					)
				}

			case float64:
				paramTypes[i+i] = byte(go_sql_driver.fieldTypeDouble)
				paramTypes[i+i+1] = 0x00

				if cap(paramValues)-len(paramValues)-8 >= 0 {
					paramValues = paramValues[:len(paramValues)+8]
					binary.LittleEndian.PutUint64(
						paramValues[len(paramValues)-8:],
						math.Float64bits(v),
					)
				} else {
					paramValues = append(paramValues,
						go_sql_driver.uint64ToBytes(math.Float64bits(v))...,
					)
				}

			case bool:
				paramTypes[i+i] = byte(go_sql_driver.fieldTypeTiny)
				paramTypes[i+i+1] = 0x00

				if v {
					paramValues = append(paramValues, 0x01)
				} else {
					paramValues = append(paramValues, 0x00)
				}

			case []byte:
				// Common case (non-nil value) first
				if v != nil {
					paramTypes[i+i] = byte(go_sql_driver.fieldTypeString)
					paramTypes[i+i+1] = 0x00

					if len(v) < longDataSize {
						paramValues = go_sql_driver.appendLengthEncodedInteger(paramValues,
							uint64(len(v)),
						)
						paramValues = append(paramValues, v...)
					} else {
						if err := stmt.writeCommandLongData(i, v); err != nil {
							return err
						}
					}
					continue
				}

				// Handle []byte(nil) as a NULL value
				nullMask[i/8] |= 1 << (uint(i) & 7)
				paramTypes[i+i] = byte(go_sql_driver.fieldTypeNULL)
				paramTypes[i+i+1] = 0x00

			case string:
				paramTypes[i+i] = byte(go_sql_driver.fieldTypeString)
				paramTypes[i+i+1] = 0x00

				if len(v) < longDataSize {
					paramValues = go_sql_driver.appendLengthEncodedInteger(paramValues,
						uint64(len(v)),
					)
					paramValues = append(paramValues, v...)
				} else {
					if err := stmt.writeCommandLongData(i, []byte(v)); err != nil {
						return err
					}
				}

			case time.Time:
				paramTypes[i+i] = byte(go_sql_driver.fieldTypeString)
				paramTypes[i+i+1] = 0x00

				var a [64]byte
				var b = a[:0]

				if v.IsZero() {
					b = append(b, "0000-00-00"...)
				} else {
					b = v.In(mc.cfg.Loc).AppendFormat(b, go_sql_driver.timeFormat)
				}

				paramValues = go_sql_driver.appendLengthEncodedInteger(paramValues,
					uint64(len(b)),
				)
				paramValues = append(paramValues, b...)

			default:
				return fmt.Errorf("cannot convert type: %T", arg)
			}
		}

		// Check if param values exceeded the available buffer
		// In that case we must build the data packet with the new values buffer
		if valuesCap != cap(paramValues) {
			data = append(data[:pos], paramValues...)
			mc.buf.buf = data
		}

		pos += len(paramValues)
		data = data[:pos]
	}

	return mc.writePacket(data)
}

func (mc *go_sql_driver.mysqlConn) discardResults() error {
	for mc.status&go_sql_driver.statusMoreResultsExists != 0 {
		resLen, err := mc.readResultSetHeaderPacket()
		if err != nil {
			return err
		}
		if resLen > 0 {
			// columns
			if err := mc.readUntilEOF(); err != nil {
				return err
			}
			// rows
			if err := mc.readUntilEOF(); err != nil {
				return err
			}
		}
	}
	return nil
}

// http://dev.mysql.com/doc/internals/en/binary-protocol-resultset-row.html
func (rows *go_sql_driver.binaryRows) readRow(dest []driver.Value) error {
	data, err := rows.mc.readPacket()
	if err != nil {
		return err
	}

	// packet indicator [1 byte]
	if data[0] != go_sql_driver.iOK {
		// EOF Packet
		if data[0] == go_sql_driver.iEOF && len(data) == 5 {
			rows.mc.status = readStatus(data[3:])
			rows.rs.done = true
			if !rows.HasNextResultSet() {
				rows.mc = nil
			}
			return io.EOF
		}
		mc := rows.mc
		rows.mc = nil

		// Error otherwise
		return mc.handleErrorPacket(data)
	}

	// NULL-bitmap,  [(column-count + 7 + 2) / 8 bytes]
	pos := 1 + (len(dest)+7+2)>>3
	nullMask := data[1:pos]

	for i := range dest {
		// Field is NULL
		// (byte >> bit-pos) % 2 == 1
		if ((nullMask[(i+2)>>3] >> uint((i+2)&7)) & 1) == 1 {
			dest[i] = nil
			continue
		}

		// Convert to byte-coded string
		switch rows.rs.columns[i].fieldType {
		case go_sql_driver.fieldTypeNULL:
			dest[i] = nil
			continue

		// Numeric Types
		case go_sql_driver.fieldTypeTiny:
			if rows.rs.columns[i].flags&go_sql_driver.flagUnsigned != 0 {
				dest[i] = int64(data[pos])
			} else {
				dest[i] = int64(int8(data[pos]))
			}
			pos++
			continue

		case go_sql_driver.fieldTypeShort, go_sql_driver.fieldTypeYear:
			if rows.rs.columns[i].flags&go_sql_driver.flagUnsigned != 0 {
				dest[i] = int64(binary.LittleEndian.Uint16(data[pos : pos+2]))
			} else {
				dest[i] = int64(int16(binary.LittleEndian.Uint16(data[pos : pos+2])))
			}
			pos += 2
			continue

		case go_sql_driver.fieldTypeInt24, go_sql_driver.fieldTypeLong:
			if rows.rs.columns[i].flags&go_sql_driver.flagUnsigned != 0 {
				dest[i] = int64(binary.LittleEndian.Uint32(data[pos : pos+4]))
			} else {
				dest[i] = int64(int32(binary.LittleEndian.Uint32(data[pos : pos+4])))
			}
			pos += 4
			continue

		case go_sql_driver.fieldTypeLongLong:
			if rows.rs.columns[i].flags&go_sql_driver.flagUnsigned != 0 {
				val := binary.LittleEndian.Uint64(data[pos : pos+8])
				if val > math.MaxInt64 {
					dest[i] = go_sql_driver.uint64ToString(val)
				} else {
					dest[i] = int64(val)
				}
			} else {
				dest[i] = int64(binary.LittleEndian.Uint64(data[pos : pos+8]))
			}
			pos += 8
			continue

		case go_sql_driver.fieldTypeFloat:
			dest[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[pos : pos+4]))
			pos += 4
			continue

		case go_sql_driver.fieldTypeDouble:
			dest[i] = math.Float64frombits(binary.LittleEndian.Uint64(data[pos : pos+8]))
			pos += 8
			continue

		// Length coded Binary Strings
		case go_sql_driver.fieldTypeDecimal, go_sql_driver.fieldTypeNewDecimal, go_sql_driver.fieldTypeVarChar,
			go_sql_driver.fieldTypeBit, go_sql_driver.fieldTypeEnum, go_sql_driver.fieldTypeSet, go_sql_driver.fieldTypeTinyBLOB,
			go_sql_driver.fieldTypeMediumBLOB, go_sql_driver.fieldTypeLongBLOB, go_sql_driver.fieldTypeBLOB,
			go_sql_driver.fieldTypeVarString, go_sql_driver.fieldTypeString, go_sql_driver.fieldTypeGeometry, go_sql_driver.fieldTypeJSON:
			var isNull bool
			var n int
			dest[i], isNull, n, err = go_sql_driver.readLengthEncodedString(data[pos:])
			pos += n
			if err == nil {
				if !isNull {
					continue
				} else {
					dest[i] = nil
					continue
				}
			}
			return err

		case
			go_sql_driver.fieldTypeDate, go_sql_driver.fieldTypeNewDate,       // Date YYYY-MM-DD
			go_sql_driver.fieldTypeTime,                                       // Time [-][H]HH:MM:SS[.fractal]
			go_sql_driver.fieldTypeTimestamp, go_sql_driver.fieldTypeDateTime: // Timestamp YYYY-MM-DD HH:MM:SS[.fractal]

			num, isNull, n := go_sql_driver.readLengthEncodedInteger(data[pos:])
			pos += n

			switch {
			case isNull:
				dest[i] = nil
				continue
			case rows.rs.columns[i].fieldType == go_sql_driver.fieldTypeTime:
				// database/sql does not support an equivalent to TIME, return a string
				var dstlen uint8
				switch decimals := rows.rs.columns[i].decimals; decimals {
				case 0x00, 0x1f:
					dstlen = 8
				case 1, 2, 3, 4, 5, 6:
					dstlen = 8 + 1 + decimals
				default:
					return fmt.Errorf(
						"protocol error, illegal decimals value %d",
						rows.rs.columns[i].decimals,
					)
				}
				dest[i], err = go_sql_driver.formatBinaryTime(data[pos:pos+int(num)], dstlen)
			case rows.mc.parseTime:
				dest[i], err = go_sql_driver.parseBinaryDateTime(num, data[pos:], rows.mc.cfg.Loc)
			default:
				var dstlen uint8
				if rows.rs.columns[i].fieldType == go_sql_driver.fieldTypeDate {
					dstlen = 10
				} else {
					switch decimals := rows.rs.columns[i].decimals; decimals {
					case 0x00, 0x1f:
						dstlen = 19
					case 1, 2, 3, 4, 5, 6:
						dstlen = 19 + 1 + decimals
					default:
						return fmt.Errorf(
							"protocol error, illegal decimals value %d",
							rows.rs.columns[i].decimals,
						)
					}
				}
				dest[i], err = go_sql_driver.formatBinaryDateTime(data[pos:pos+int(num)], dstlen)
			}

			if err == nil {
				pos += int(num)
				continue
			} else {
				return err
			}

		// Please report if this happens!
		default:
			return fmt.Errorf("unknown field type %d", rows.rs.columns[i].fieldType)
		}
	}

	return nil
}
