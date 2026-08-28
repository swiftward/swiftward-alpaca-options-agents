package execution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

// Всё, что лестница говорит СЕССИИ, попадает в запись и оттуда на страницу,
// которую открывает судья. Значит по-английски.
//
// Разница с харнессом не в моде, а в адресате: его "готово за 18 с" уходит в наш
// Telegram, и там русский на месте. Здесь адресат другой.
//
// Проверка нужна потому, что эта граница уже терялась: 29 августа тридцать ходов
// из пятидесяти несли русскую причину запуска, и половина из них родилась прямо
// здесь - "Заявки, которые ты отправил, сняты по терпению". Заметил это монитор,
// а не человек, и заметил случайно.
func TestNothingTheSessionIsToldIsInRussian(t *testing.T) {
	for _, name := range []string{"execution.go"} {
		raw, err := os.ReadFile(filepath.Join(".", name))
		require.NoError(t, err)

		for number, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			// Комментарии объясняют, зачем написано, и читаем их мы.
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, piece := range quoted(line) {
				for _, r := range piece {
					if unicode.Is(unicode.Cyrillic, r) {
						t.Errorf("%s:%d говорит сессии по-русски, а это читает судья: %s",
							name, number+1, trimmed)
						break
					}
				}
			}
		}
	}
}

// quoted вытаскивает содержимое строковых литералов, чтобы кириллица в
// комментарии на конце строки не считалась нарушением.
func quoted(line string) []string {
	var out []string
	inside, start := false, 0
	for i, r := range line {
		if r != '"' {
			continue
		}
		if i > 0 && line[i-1] == '\\' {
			continue
		}
		if inside {
			out = append(out, line[start:i])
			inside = false
			continue
		}
		inside, start = true, i+1
	}

	return out
}
