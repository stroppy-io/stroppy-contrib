#!/usr/bin/env tclsh
# Run the pinned timed driver without its GUI; six-second polling remains unchanged.
# Arguments: checkout host port user database text|prepared clients rampup-minutes duration-minutes.
# MYSQL_PWD supplies the password without putting it in process arguments.
package require Thread
lassign $argv root host port user db profile clients rampup duration
if {$profile ni {text prepared} || ![string is integer -strict $clients] || $clients < 1} { error "expected text|prepared and positive client count" }
foreach value [list $rampup $duration] {
    if {![string is double -strict $value] || $value < 0 || abs($value*60000 - round($value*60000/6000)*6000) > 0.001} {
        error "timings must be nonnegative multiples of 0.1 minutes (HammerDB polls every six seconds)"
    }
}
if {$duration <= 0} { error "duration must be positive" }
set password $::env(MYSQL_PWD)
set channel [open [file join $root src mysql mysqloltp.tcl] r]
set text [read $channel]
close $channel
set start [string first {if [catch {package require $library}} $text [string first "proc loadtimedmysqltpcc" $text]]
set end [string first "\n\}\}\nif \{ \$mysql_connect_pool" $text $start]
if {$start < 0 || $end < 0} { error "pinned HammerDB timed script boundaries not found" }
set script [string range $text $start [expr {$end+1}]]
# The GUI normally assigns these identities; the standalone launcher supplies them.
set script [string map [list {[ findvuposition ]} {[list $position $totalvirtualusers]}] $script]
set header ""
foreach {key value} [list library mysqltcl total_iterations 2147483647 RAISEERROR true KEYANDTHINK false rampup $rampup duration $duration mode Local host $host port $port socket null ssl_options {-ssl false} user $user password $password db $db prepare [expr {$profile eq "prepared"}]] {
    append header [list set $key $value] "\n"
}
append header [list source [file join $root modules tpcccommon-1.0.tm]] "\n"
tsv::set application abort 0
set threads {}
for {set i 1} {$i <= $clients+1} {incr i} {
    set id [thread::create -joinable]
    lappend threads $id
    set payload $header
    append payload [list set position $i] "\n" [list set totalvirtualusers [expr {$clients+1}]] "\n"
    # Client seeds match the SDK's explicit worker streams; the monitor consumes no draws.
    append payload "expr {srand([expr {max(1,$i-1)}])}\n" $script
    set guarded "set code \[catch [list $payload] result\]\nlist \$code \$result"
    thread::send -async $id $guarded finished($i)
}
set failures {}
for {set i 1} {$i <= $clients+1} {incr i} {
    if {![info exists finished($i)]} { vwait finished($i) }
    lassign $finished($i) code result
    if {$code != 0} { lappend failures "thread $i: $result"; tsv::set application abort 1 }
}
foreach id $threads { thread::release $id; thread::join $id }
if {[llength $failures]} { error [join $failures "\n"] }
