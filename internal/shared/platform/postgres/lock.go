package postgres

import "hash/crc64"

var lockTable = crc64.MakeTable(crc64.ECMA)

func AdvisoryLockKey(name string) int64 {
	return int64(crc64.Checksum([]byte(name), lockTable) >> 1)
}
