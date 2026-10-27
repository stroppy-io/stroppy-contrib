package workload

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// random uses Tcl's Park-Miller generator, including the initial srand draw.
// Each HammerDB thread owns one stream; asynchronous clients share their thread's stream.
type random struct{ state int64 }

func newRandom(seed int64) *random {
	seed = seed % 2147483647
	if seed <= 0 {
		seed += 2147483646
	}
	r := &random{state: seed}
	r.float()
	return r
}

func (r *random) float() float64 {
	r.state = (r.state * 16807) % 2147483647
	return float64(r.state) / 2147483647
}

func (r *random) number(low, high int) int {
	return int(float64(low) + r.float()*float64(high+1-low))
}

func (r *random) nurand() int {
	return ((r.number(0, 255) | r.number(0, 999)) + 123) % 1000
}

func lastname(n int) string {
	parts := [...]string{"BAR", "OUGHT", "ABLE", "PRI", "PRES", "ESE", "ANTI", "CALLY", "ATION", "EING"}
	return parts[n/100%10] + parts[n/10%10] + parts[n%10]
}

func (r *random) alpha(low, high int) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	length := r.number(low, high)
	var result strings.Builder
	result.Grow((length + 3) / 4 * 4)
	for i := 0; i < length; i += 4 {
		n := int(r.float() * (62 * 62 * 62 * 62))
		for _, divisor := range [...]int{62 * 62 * 62, 62 * 62, 62, 1} {
			result.WriteByte(alphabet[n/divisor%62])
		}
	}
	return result.String()[:length]
}

// prefixNumber reproduces Tcl's replacement of a zero prefix, not zero padding.
func prefixNumber(value, width int) string {
	n := strconv.Itoa(value)
	return n + strings.Repeat("0", width-len(n))
}

func (r *random) zip() string {
	n := strconv.Itoa(r.number(0, 9999))
	return n + "000011111"[len(n):]
}

func (r *random) phone() string {
	return prefixNumber(r.number(0, 99999999), 8) + prefixNumber(r.number(0, 99999999), 8)
}

func (r *random) address() address {
	return address{r.alpha(10, 20), r.alpha(10, 20), r.alpha(10, 20), r.alpha(2, 2), r.zip()}
}

func (r *random) think(mean int) time.Duration {
	return time.Duration(math.Abs(math.Round(math.Log(r.float())*float64(mean)))) * time.Second
}

func (r *random) originals(items int) map[int]bool {
	selected := make(map[int]bool, items/10)
	for i := 0; i < items/10; i++ {
		selected[r.number(0, items)] = true
	}
	return selected
}

// Tcl string replace includes both endpoints, so "original" replaces nine bytes.
func (r *random) original(data string, selected bool) string {
	if !selected {
		return data
	}
	first := r.number(0, len(data)-8)
	return data[:first] + "original" + data[min(first+9, len(data)):]
}

type address struct{ Street1, Street2, City, State, Zip string }
