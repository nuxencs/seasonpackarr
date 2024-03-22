package torrents

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"
)

func DecodeTorrentDataRawBytes(torrentBytes []byte) ([]byte, error) {
	var tb []byte
	var err error

	if tb, err = base64.StdEncoding.DecodeString(strings.Trim(strings.TrimSpace(string(torrentBytes)), `"`)); err == nil {
		return tb, nil
	} else {
		ts := strings.Trim(strings.TrimSpace(string(torrentBytes)), `\"[`)
		b := make([]byte, 0, len(ts)/3)

		for {
			r, valid, z := atoi(ts)
			if !valid {
				break
			}

			b = append(b, byte(r))
			ts = z
		}

		if len(b) != 0 {
			return b, nil
		}
	}

	return []byte{}, fmt.Errorf("couldn't decode torrent bytes")
}

func atoi(buf string) (ret int, valid bool, pos string) {
	if len(buf) == 0 {
		return ret, false, buf
	}

	i := 0
	for ; unicode.IsSpace(rune(buf[i])); i++ {
	}

	r := buf[i]
	if r == '-' || r == '+' {
		i++
	}

	for ; i != len(buf); i++ {
		d := int(buf[i] - '0')
		if d < 0 || d > 9 {
			break
		}

		valid = true
		ret *= 10
		ret += d
	}

	if r == '-' {
		ret *= -1
	}

	return ret, valid, buf[i:]
}
