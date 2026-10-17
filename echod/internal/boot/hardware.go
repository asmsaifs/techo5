package boot

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/HuskerMinion/techo5/echod/internal/layout"
)

func listenAddr() string { return fmt.Sprintf(":%d", layout.Port) }

func name() string {
	if recorded := nameIn(layout.NamePath); recorded != "" {
		return recorded
	}

	mac, err := layout.FactoryMAC()
	if err != nil {
		return layout.DefaultName
	}
	return layout.NameFromMAC(mac)
}

// nameIn is the name recorded in the name file at path, empty with none. One not in UTF-8 is read as
// Windows-1252 (readableName) and written back as UTF-8, once, so the next start reads it as it is
// rather than warning again; the name read is used either way.
func nameIn(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := readableName(b)
	if !utf8.Valid(b) {
		if err := rewriteName(path, text); err != nil {
			slog.Warn("writing the device name back as UTF-8 failed; using it as read", "err", err)
		} else {
			slog.Info("the device name was written back as UTF-8")
		}
	}
	return strings.TrimSpace(text)
}

// rewriteName replaces the name file at path with text, with the permissions it had: written beside it
// and renamed over it, so a power cut leaves the old file or the new one, never half of one.
func rewriteName(path, text string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // nothing once it is renamed
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// readableName is the name file as text. An installer run on Windows wrote it in the Windows code page,
// so "Küche" arrived as one 0xFC byte rather than UTF-8. Go's protobuf will not send a string that is not
// UTF-8, so the device could not tell Home Assistant its name and every connection dropped after the
// handshake; D-Bus refuses it too. Such a name is read as Windows-1252, the code page of the western
// languages: Latin-1 but for 0x80 to 0x9F, where it keeps letters like Š and Œ.
func readableName(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
		if c >= 0x80 && c < 0xa0 && cp1252[c-0x80] != 0 {
			r[i] = cp1252[c-0x80]
		}
	}
	slog.Warn("the device name is not UTF-8; reading it as Windows-1252", "name", string(r))
	return string(r)
}

// cp1252 is what Windows-1252 has at 0x80 to 0x9F; 0 where it has nothing, kept as the Latin-1 control.
var cp1252 = [32]rune{
	'€', 0, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0, 'Ž', 0,
	0, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0, 'ž', 'Ÿ',
}
