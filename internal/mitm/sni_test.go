package mitm

import (
	"bufio"
	"bytes"
	"testing"
)

func TestPeekSNIReadsFullRecord(t *testing.T) {
	// Minimal TLS record + ClientHello with SNI past the old 1024-byte peek window.
	sni := "lobby.winfinity.live"
	ch := buildClientHelloWithSNI(sni, 1200) // padding so SNI sits after offset 1024
	record := make([]byte, 5+len(ch))
	record[0] = 0x16
	record[1] = 0x03
	record[2] = 0x01
	record[3] = byte(len(ch) >> 8)
	record[4] = byte(len(ch))
	copy(record[5:], ch)

	br := bufio.NewReader(bytes.NewReader(record))
	got, err := peekSNI(br)
	if err != nil {
		t.Fatal(err)
	}
	if got != sni {
		t.Fatalf("got %q want %q", got, sni)
	}
}

// buildClientHelloWithSNI crafts a ClientHello where the SNI extension starts
// after padBeforeSNI bytes of the handshake message (excluding TLS record header).
func buildClientHelloWithSNI(hostname string, padBeforeSNI int) []byte {
	if padBeforeSNI < 0 {
		padBeforeSNI = 0
	}
	// handshake header + legacy ClientHello prefix up to extensions
	// type=1, len=placeholder, version, random(32), session_id=0, cipher_suites, compression, extensions
	prefix := []byte{
		0x01,             // client_hello
		0x00, 0x00, 0x00, // length placeholder
		0x03, 0x03, // version
	}
	prefix = append(prefix, make([]byte, 32)...)    // random
	prefix = append(prefix, 0x00)                   // session_id len
	prefix = append(prefix, 0x00, 0x02, 0x00, 0x2f) // cipher_suites: TLS_RSA_WITH_AES_128_CBC_SHA
	prefix = append(prefix, 0x01, 0x00)             // compression methods: null

	// extensions: optional padding (type 21) then SNI (type 0)
	var exts []byte
	if padBeforeSNI > 0 {
		// extension type 21 = padding; length = padBeforeSNI; zeros
		exts = append(exts, 0x00, 0x15)
		exts = append(exts, byte(padBeforeSNI>>8), byte(padBeforeSNI))
		exts = append(exts, make([]byte, padBeforeSNI)...)
	}
	host := []byte(hostname)
	sni := []byte{0x00, 0x00} // type SNI
	sniBody := []byte{
		0x00, byte(1 + 2 + len(host)), // server_name_list length later overwritten
		0x00, // host_name
		byte(len(host) >> 8), byte(len(host)),
	}
	sniBody = append(sniBody[:0], 0x00, byte(3+len(host))) // list len = name_type(1)+name_len(2)+name
	sniBody = append(sniBody, 0x00)
	sniBody = append(sniBody, byte(len(host)>>8), byte(len(host)))
	sniBody = append(sniBody, host...)
	sni = append(sni, byte(len(sniBody)>>8), byte(len(sniBody)))
	sni = append(sni, sniBody...)
	exts = append(exts, sni...)

	prefix = append(prefix, byte(len(exts)>>8), byte(len(exts)))
	prefix = append(prefix, exts...)

	hsLen := len(prefix) - 4
	prefix[1] = byte(hsLen >> 16)
	prefix[2] = byte(hsLen >> 8)
	prefix[3] = byte(hsLen)
	return prefix
}
