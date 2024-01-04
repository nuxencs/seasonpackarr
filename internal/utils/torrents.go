package utils

import (
	"bytes"
	"encoding/base64"
	"strings"
	"unicode"

	"github.com/anacrolix/torrent/metainfo"
)

func ParseFolderNameFromTorrentBytes(torrentBytes []byte) (string, error) {
	r := bytes.NewReader(torrentBytes)

	metaInfo, err := metainfo.Load(r)
	if err != nil {
		return "", err
	}

	info, err := metaInfo.UnmarshalInfo()
	if err != nil {
		return "", err
	}

	return info.BestName(), nil
}

func DecodeTorrentDataRawBytes(torrentBytes []byte) ([]byte, error) {
	var tb []byte
	var err error

	if tb, err = base64.StdEncoding.DecodeString(strings.Trim(strings.TrimSpace(string(torrentBytes)), `"`)); err == nil {
		return tb, nil
	} else {
		ts := strings.Trim(strings.TrimSpace(string(torrentBytes)), `\"[`)
		b := make([]byte, 0, len(ts)/3)

		for {
			r, valid, z := Atoi(ts)
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

	return []byte{}, err
}

func Atoi(buf string) (ret int, valid bool, pos string) {
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
