package network

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestPCPMapProtocolMock(t *testing.T) {
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	go func() {
		buf := make([]byte, 128)
		for {
			n, rAddr, err := pc.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n < 60 || buf[0] != 2 || buf[1] != 1 {
				continue
			}
			resp := make([]byte, 60)
			resp[0] = 2
			resp[1] = 0x81
			resp[3] = 0 // Success
			copy(resp[4:8], buf[4:8])
			copy(resp[24:36], buf[24:36]) // echo nonce
			resp[36] = buf[36]
			copy(resp[40:42], buf[40:42])
			copy(resp[42:44], buf[40:42]) // ext port = internal port
			extIP := net.ParseIP("203.0.113.42").To16()
			copy(resp[44:60], extIP)
			_, _ = pc.WriteToUDP(resp, rAddr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := pcpMapPort(ctx, pc.LocalAddr().String(), "192.168.1.50", 25565, "TCP", 7200); err != nil {
		t.Fatalf("pcpMapPort failed: %v", err)
	}
}

func TestNATPMPProtocolMock(t *testing.T) {
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	go func() {
		buf := make([]byte, 64)
		for {
			n, rAddr, err := pc.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n >= 2 && buf[0] == 0 && buf[1] == 0 {
				resp := make([]byte, 12)
				resp[0], resp[1] = 0, 0x80
				copy(resp[8:12], []byte{203, 0, 113, 99})
				_, _ = pc.WriteToUDP(resp, rAddr)
			} else if n >= 12 && buf[0] == 0 && (buf[1] == 1 || buf[1] == 2) {
				resp := make([]byte, 16)
				resp[0], resp[1] = 0, 0x80|buf[1]
				copy(resp[8:10], buf[4:6])
				copy(resp[10:12], buf[6:8])
				binary.BigEndian.PutUint32(resp[12:16], 3600)
				_, _ = pc.WriteToUDP(resp, rAddr)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ip, ok := probeNATPMPExternalIP(ctx, pc.LocalAddr().String())
	if !ok || ip != "203.0.113.99" {
		t.Fatalf("probeNATPMPExternalIP mismatch: ok=%v ip=%q", ok, ip)
	}
	if err := natPmpMapPort(ctx, pc.LocalAddr().String(), 19132, "UDP", 3600); err != nil {
		t.Fatalf("natPmpMapPort failed: %v", err)
	}
}
