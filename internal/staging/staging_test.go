package staging

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStageAndRead(t *testing.T) {
	s, err := New(t.TempDir(), time.Minute, 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	info, err := s.Stage(strings.NewReader("# 标题\n\n正文\n"), "dir/report.md")
	if err != nil {
		t.Fatal(err)
	}
	if info.Filename != "report.md" {
		t.Fatalf("filename = %q, want base name", info.Filename)
	}
	if info.Size != int64(len("# 标题\n\n正文\n")) || len(info.SHA256) != 64 || len(info.ID) != 32 {
		t.Fatalf("info = %#v", info)
	}

	got, err := s.Read(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# 标题\n\n正文\n" {
		t.Fatalf("content = %q", got)
	}

	// Read is not consuming: a retry works.
	if _, err := s.Read(info.ID); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if err := s.Delete(info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(info.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestStageRejections(t *testing.T) {
	s, err := New(t.TempDir(), time.Minute, 8)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		body []byte
		want error
	}{
		{"empty", nil, ErrEmpty},
		{"too large", []byte("0123456789"), ErrTooLarge},
		{"binary", []byte{0xff, 0xfe, 0x00}, ErrNotText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Stage(bytes.NewReader(tc.body), "x.md")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestStageExpires(t *testing.T) {
	s, err := New(t.TempDir(), 10*time.Millisecond, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Stage(strings.NewReader("body"), "x.md")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := s.Read(info.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUnknownID(t *testing.T) {
	s, err := New(t.TempDir(), time.Minute, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := s.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete err = %v", err)
	}
}
