package purego

// MapFindNext 查找映射中的下一个已使用页
func (mdb *MdbHandle) MapFindNext(usageMap []byte, mapSize int, startPg int) int {
	if usageMap == nil || mapSize < 5 {
		return 0
	}
	if usageMap[0] == 0 {
		return int(mdb.mapFindNext0(usageMap, mapSize, uint32(startPg)))
	} else if usageMap[0] == 1 {
		return int(mdb.mapFindNext1(usageMap, mapSize, uint32(startPg)))
	}
	return 0
}

// mapFindNext0 处理类型0的使用映射（简单位图）
func (mdb *MdbHandle) mapFindNext0(map_ []byte, mapSz int, startPg uint32) uint32 {
	pgnum := uint32(GetInt32(map_, 1))
	usageBitmap := map_[5:]
	usageBitlen := uint32((mapSz - 5) * 8)

	var i uint32
	if startPg >= pgnum {
		i = startPg - pgnum + 1
	} else {
		i = 0
	}
	for ; i < usageBitlen; i++ {
		if usageBitmap[i/8]&(1<<(i%8)) != 0 {
			return pgnum + i
		}
	}
	return 0
}

// mapFindNext1 处理类型1的使用映射（多页位图）
func (mdb *MdbHandle) mapFindNext1(map_ []byte, mapSz int, startPg uint32) uint32 {
	usageBitlen := uint32((mdb.Fmt.PgSize - 4) * 8)
	maxMapPgs := uint32((mapSz - 1) / 4)
	mapInd := (startPg + 1) / usageBitlen
	offset := (startPg + 1) % usageBitlen

	for ; mapInd < maxMapPgs; mapInd++ {
		mapPg := uint32(GetInt32(map_, int(mapInd*4+1)))
		if mapPg == 0 {
			continue
		}
		if mdb.ReadAltPg(mapPg) != mdb.Fmt.PgSize {
			return 0
		}

		usageBitmap := mdb.AltPgBuf[4:]
		var i uint32
		for i = offset; i < usageBitlen; i++ {
			if usageBitmap[i/8]&(1<<(i%8)) != 0 {
				return mapInd*usageBitlen + i
			}
		}
		offset = 0
	}
	return 0
}

// MapFindNextFreepage 查找下一个空闲页
func (mdb *MdbHandle) MapFindNextFreepage(table *MdbTableDef, rowSize int) int {
	return mdb.MapFindNext(table.FreeUsageMap, table.FreemapSize, int(table.FreemapBasePg))
}
