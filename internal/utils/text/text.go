package text

import (
	"fmt"
	"regexp"
	"time"
)

func DistanceLevenshtein(s1, s2 string) int {
	d := make([][]int, len(s1)+1)
	for i := range d {
		d[i] = make([]int, len(s2)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cout := 0
			if s1[i-1] != s2[j-1] {
				cout = 1
			}
			a, b, c := d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cout
			if a < b {
				if a < c {
					d[i][j] = a
				} else {
					d[i][j] = c
				}
			} else if b < c {
				d[i][j] = b
			} else {
				d[i][j] = c
			}
		}
	}
	return d[len(s1)][len(s2)]
}

func DetecterHeure(texte string) (time.Time, bool) {
	re := regexp.MustCompile(`(\d{1,2})h(\d{2})?`)
	m := re.FindStringSubmatch(texte)
	if len(m) < 2 {
		return time.Time{}, false
	}

	minuteStr := "00"
	if len(m) > 2 && m[2] != "" {
		minuteStr = m[2]
	}

	var h, min int
	if _, err := fmt.Sscanf(m[1], "%d", &h); err != nil {
		return time.Time{}, false
	}
	if _, err := fmt.Sscanf(minuteStr, "%d", &min); err != nil {
		return time.Time{}, false
	}

	if h < 0 || h >= 24 || min < 0 || min >= 60 {
		return time.Time{}, false
	}

	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), h, min, 0, 0, time.Local), true
}
