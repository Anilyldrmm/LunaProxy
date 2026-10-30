package main

// parseSNI — tek bir TLS kaydı içindeki ClientHello'dan SNI'yı ve kayıt içindeki
// başlangıç ofsetini döndürür. ClientHello değilse veya SNI yoksa ok=false.
func parseSNI(rec []byte) (name string, off int, ok bool) {
	// kayıt başlığı (5) + handshake başlığı (4) + version (2) + random (32)
	if len(rec) < 5+4+2+32+1 || rec[0] != 0x16 || rec[5] != 0x01 {
		return "", 0, false
	}
	p := 5 + 4 + 2 + 32
	skip := func(lenBytes int) bool {
		if p+lenBytes > len(rec) {
			return false
		}
		n := 0
		for i := 0; i < lenBytes; i++ {
			n = n<<8 | int(rec[p+i])
		}
		p += lenBytes + n
		return p <= len(rec)
	}
	// session id, cipher suites, compression methods
	if !skip(1) || !skip(2) || !skip(1) {
		return "", 0, false
	}
	if p+2 > len(rec) {
		return "", 0, false
	}
	end := p + 2 + (int(rec[p])<<8 | int(rec[p+1]))
	p += 2
	if end > len(rec) {
		end = len(rec)
	}
	for p+4 <= end {
		typ := int(rec[p])<<8 | int(rec[p+1])
		l := int(rec[p+2])<<8 | int(rec[p+3])
		p += 4
		if typ == 0 && p+5 <= end { // server_name: list len(2) type(1) name len(2) name
			nl := int(rec[p+3])<<8 | int(rec[p+4])
			if rec[p+2] == 0 && nl > 0 && p+5+nl <= end {
				return string(rec[p+5 : p+5+nl]), p + 5, true
			}
			return "", 0, false
		}
		p += l
	}
	return "", 0, false
}

// splitTLSRecord — tek TLS kaydını at noktasından (kayıt ofseti, >5) iki kayda böler.
func splitTLSRecord(rec []byte, at int) ([]byte, []byte) {
	r1 := append([]byte{rec[0], rec[1], rec[2], byte((at - 5) >> 8), byte(at - 5)}, rec[5:at]...)
	n2 := len(rec) - at
	r2 := append([]byte{rec[0], rec[1], rec[2], byte(n2 >> 8), byte(n2)}, rec[at:]...)
	return r1, r2
}
