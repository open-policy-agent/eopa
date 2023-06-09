package wait

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func ForLog(t *testing.T, buf *bytes.Buffer, assert func(string) bool, dur time.Duration) {
	t.Helper()
	for i := 0; i <= 3; i++ {
		if retrieveMsg(t, buf, assert) {
			return
		}
		time.Sleep(dur)
	}
	t.Fatalf("timeout waiting for log")
}

func retrieveMsg(t *testing.T, buf *bytes.Buffer, assert func(string) bool) bool {
	t.Helper()
	if _, err := buf.ReadBytes('{'); err == nil {
		if err := buf.UnreadByte(); err != nil {
			t.Fatal(err)
		}
	}
	b := bytes.NewReader(buf.Bytes())
	scanner := bufio.NewScanner(b)
	for scanner.Scan() {
		line := scanner.Bytes()
		var m struct {
			Msg string
		}
		if err := json.NewDecoder(bytes.NewReader(line)).Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode console logs: %v", err)
		}
		if assert(m.Msg) {
			return true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return false
}
