package path

import (
	"path/filepath"
	"testing"
)

const P = "/wine/prefix"

func mustTranslatePathToWine(t *testing.T, windowsPath string) string {
	got, err := TranslatePathToWine(P, windowsPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return got
}

func mustErrKind(t *testing.T, err error, wantKind PathErrorKind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error kind=%q, got nil", wantKind)
	}
	pe, ok := err.(*PathError)
	if !ok {
		t.Fatalf("expected PathError, got %T", err)
	}
	if pe.Kind != wantKind {
		t.Fatalf("expected kind=%q, got %q", wantKind, pe.Kind)
	}
}

func TestTranslate_DriveBasic(t *testing.T) {
	got := mustTranslatePathToWine(t, `C:\Program Files\Foo\bar.txt`)
	want := filepath.Join(P, "drive_c", "Program Files", "Foo", "bar.txt")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTranslate_MixedSlashesAndCase(t *testing.T) {
	got := mustTranslatePathToWine(t, `c:/Windows\System32\cmd.exe`)
	want := filepath.Join(P, "drive_c", "Windows", "System32", "cmd.exe")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTranslate_DriveOnlyRoot(t *testing.T) {
	got, err := TranslatePathToWine(P, `D:`)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(P, "drive_d")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTranslate_DriveRelative_TreatAsAbsolute(t *testing.T) {
	got := mustTranslatePathToWine(t, `C:relative\path\file.txt`)
	want := filepath.Join(P, "drive_c", "relative", "path", "file.txt")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTranslate_ExtendedLength(t *testing.T) {
	got := mustTranslatePathToWine(t, `\\?\C:\dir\file.txt`)
	want := filepath.Join(P, "drive_c", "dir", "file.txt")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTranslate_UNC(t *testing.T) {
	got := mustTranslatePathToWine(t, `\\fileserver\share\dir\file.txt`)
	want := filepath.Join(P, "dosdevices", "unc", "fileserver", "share", "dir", "file.txt")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTranslate_UNC_Extended(t *testing.T) {
	got := mustTranslatePathToWine(t, `\\?\UNC\fs01\media\song.mp3`)
	want := filepath.Join(P, "dosdevices", "unc", "fs01", "media", "song.mp3")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTranslate_NormalizesDots(t *testing.T) {
	got := mustTranslatePathToWine(t, `C:\foo\.\bar\..\baz\qux.txt`)
	want := filepath.Join(P, "drive_c", "foo", "baz", "qux.txt")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTranslate_EmptyAndSpaces(t *testing.T) {
	_, err := TranslatePathToWine(P, "")
	mustErrKind(t, err, ErrEmpty)
	_, err = TranslatePathToWine(P, "   ")
	mustErrKind(t, err, ErrEmpty)
}

func TestTranslate_RelativeUnsupported(t *testing.T) {
	_, err := TranslatePathToWine(P, `foo\bar`)
	mustErrKind(t, err, ErrUnsupported)
}

func TestTranslate_InvalidUNC(t *testing.T) {
	_, err := TranslatePathToWine(P, `\\serveronly`)
	mustErrKind(t, err, ErrInvalidUNC)
}
