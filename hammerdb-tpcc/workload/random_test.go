package workload

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTclRandomSequence(t *testing.T) {
	r := newRandom(1)
	want := []float64{0.13153778814316625, 0.7556053221950332, 0.4586501319234493, 0.5327672374121692, 0.21895918632809036, 0.04704461621448613}
	for _, expected := range want {
		if got := r.float(); got != expected {
			t.Fatalf("random = %.17g, want %.17g", got, expected)
		}
	}
}

func TestTclGenerationParity(t *testing.T) {
	path, err := exec.LookPath("tclsh")
	if err != nil {
		t.Skip("tclsh is required for the independent generator oracle")
	}
	command := exec.CommandContext(t.Context(), path)
	command.Stdin = strings.NewReader(`
proc number {low high} {return [expr {int($low+rand()*($high+1-$low))}]}
proc alpha {low high} {
    set alphabet 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz
    set length [number $low $high]
    set result ""
    for {set i 0} {$i < $length} {incr i 4} {
        set n [expr {int(rand()*14776336)}]
        foreach divisor {238328 3844 62 1} {
            append result [string index $alphabet [expr {$n/$divisor%62}]]
        }
    }
    return [string range $result 0 $length-1]
}
expr {srand(7)}
puts [alpha 8 16]
puts [alpha 26 50]
puts [number 0 9999]
puts [number 0 99999999]
puts [number 0 99999999]
puts [expr {abs(round(log(rand())*12))}]
`)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(output))
	r := newRandom(7)
	want := []string{r.alpha(8, 16), r.alpha(26, 50), strconv.Itoa(r.number(0, 9999)), strconv.Itoa(r.number(0, 99999999)), strconv.Itoa(r.number(0, 99999999)), strconv.FormatInt(int64(r.think(12)/time.Second), 10)}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Tcl %q != Go %q", got, want)
	}
}

func TestHammerDBStringAndShuffleQuirks(t *testing.T) {
	if got := prefixNumber(123, 8); got != "12300000" {
		t.Fatal(got)
	}
	r := newRandom(1)
	if got := len(r.original(strings.Repeat("x", 26), true)); got != 25 {
		t.Fatal(got)
	}
	customers := customerOrder(newRandom(1), 3000)
	seen := map[int]bool{}
	for _, customer := range customers[1:] {
		seen[customer] = true
	}
	if len(seen) == 3000 {
		t.Fatal("upstream asymmetric shuffle was replaced by a permutation")
	}
}

func TestTransactionMix(t *testing.T) {
	var counts [5]int
	for choice := 1; choice <= 23; choice++ {
		counts[pickTransaction(choice)]++
	}
	if counts != [5]int{10, 10, 1, 1, 1} {
		t.Fatal(counts)
	}
}

func TestAllWarehouseAssignment(t *testing.T) {
	want := [][]int{{1, 4, 7, 10}, {2, 5, 8, 1}, {3, 6, 9, 2}}
	for owner, expected := range want {
		got := allWarehouses(owner, 3, 10)
		if len(got) != len(expected) {
			t.Fatal(got)
		}
		for i := range got {
			if got[i] != expected[i] {
				t.Fatalf("owner %d: %v", owner, got)
			}
		}
	}
}

func TestLoaderPopulationQuirks(t *testing.T) {
	r := newRandom(5)
	customers, history := customerRows(r, 1, 2, 1, 3000, time.Unix(0, 0))
	badCredit := 0
	for _, customer := range customers {
		if customer.Credit == "BC" {
			badCredit++
		}
		if customer.Balance != -10 || customer.YTDPayment != 10 || customer.PaymentCount != 1 {
			t.Fatal(customer)
		}
	}
	if badCredit < 1300 || badCredit > 1700 {
		t.Fatalf("bad credit %d, expected HammerDB's 50%%", badCredit)
	}
	if len(history) != 3000 {
		t.Fatal(len(history))
	}
	orders, pending, lines := orderRows(r, 1, 2, 2100, 2, customerOrder(r, 3000), time.Unix(0, 0))
	if orders[0].Carrier == nil || orders[1].Carrier != nil || len(pending) != 1 {
		t.Fatal(orders, pending)
	}
	for _, line := range lines {
		if line.Order == 2101 && (line.Amount != 0 || line.Delivery != nil) {
			t.Fatal(line)
		}
		if line.Order == 2100 && (line.Amount < 0.1 || line.Delivery == nil) {
			t.Fatal(line)
		}
	}
}

func TestPerMinuteMatchesIntegerDivision(t *testing.T) {
	if got := perMinute(101, 2*time.Minute); got != 50 {
		t.Fatal(got)
	}
	if got := perMinute(10, 30*time.Second); got != 20 {
		t.Fatal(got)
	}
}
