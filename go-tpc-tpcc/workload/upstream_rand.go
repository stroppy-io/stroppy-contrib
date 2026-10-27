package workload

import (
	"math/rand"
)

func convertToPQ(query string, driver string) string { return query }
func randInt(r *rand.Rand, min, max int) int {
	if min == max {
		return min
	}
	return r.Intn(max-min+1) + min
}

const (
	characters    = `abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890`
	letters       = `ABCDEFGHIJKLMNOPQRSTUVWXYZ`
	numbers       = `1234567890`
	lenCharacters = 62
	lenLetters    = 26
	lenNumbers    = 10
)

func randBuffer(r *rand.Rand, b *buffer, source string, min, max int, num int) []byte {
	buf := b.Alloc(randInt(r, min, max))
	for i := range buf {
		buf[i] = source[r.Intn(num)]
	}
	return buf
}
func randChars(r *rand.Rand, b *buffer, min, max int) string {
	return string(randBuffer(r, b, characters, min, max, lenCharacters))
}
func randLetters(r *rand.Rand, b *buffer, min, max int) string {
	return string(randBuffer(r, b, letters, min, max, lenLetters))
}
func randNumbers(r *rand.Rand, b *buffer, min, max int) string {
	return string(randBuffer(r, b, numbers, min, max, lenNumbers))
}
func randZip(r *rand.Rand, b *buffer) string {
	buf := randBuffer(r, b, numbers, 9, 9, lenNumbers)
	copy(buf[4:], `11111`)
	return string(buf)
}

func randState(r *rand.Rand, b *buffer) string {
	buf := randBuffer(r, b, letters, 2, 2, lenLetters)
	return string(buf)
}

func randTax(r *rand.Rand) float64 {
	return float64(randInt(r, 0, 2000)) / 10000.0
}

const originalString = "ORIGINAL"

func randOriginalString(r *rand.Rand, b *buffer) string {
	if r.Intn(10) == 0 {
		buf := randBuffer(r, b, characters, 26, 50, lenCharacters)
		index := r.Intn(len(buf) - 8)
		copy(buf[index:], originalString)
		return string(buf)
	}

	return randChars(r, b, 26, 50)
}

var cLastTokens = [...]string{
	"BAR", "OUGHT", "ABLE", "PRI", "PRES",
	"ESE", "ANTI", "CALLY", "ATION", "EING"}

func randCLastSyllables(n int, b *buffer) string {
	buf := b.Alloc(3 * 5)
	buf = buf[:0]
	buf = append(buf, cLastTokens[n/100]...)
	n = n % 100
	buf = append(buf, cLastTokens[n/10]...)
	n = n % 10
	buf = append(buf, cLastTokens[n]...)
	return string(buf)
}

func randCLast(r *rand.Rand, b *buffer, cLoad int) string {
	return randCLastSyllables(((r.Intn(256)|r.Intn(1000))+cLoad)%1000, b)
}
func randCustomerID(r *rand.Rand, cCustomerID int) int {
	return ((r.Intn(1024) | (r.Intn(3000) + 1) + cCustomerID) % 3000) + 1
}
func randItemID(r *rand.Rand, cItemID int) int {
	return ((r.Intn(8190) | (r.Intn(100000) + 1) + cItemID) % 100000) + 1
}
