#!/usr/bin/env tclsh
# Run the pinned HammerDB synchronous client without the GUI.
# Arguments: reference checkout, libpq connection string, functions|procedures, iterations.
package require Pgtcl
package require Thread
set root [lindex $argv 0]
set conninfo [lindex $argv 1]
set procedures [expr {[lindex $argv 2] eq "procedures" ? "true" : "false"}]
set iterations [lindex $argv 3]
# Debian's Pgtcl binding omits HammerDB's two format-list arguments.
if {[lindex $argv 4] eq "legacy-pgtcl"} {
    rename pg_exec_prepared pg_exec_prepared_native
    proc pg_exec_prepared {connection name resultFormats parameterFormats args} {
        return [pg_exec_prepared_native $connection $name {*}$args]
    }
}
set channel [open [file join $root src postgresql pgoltp.tcl] r]
set source [read $channel]
close $channel
source [file join $root modules tpcccommon-1.0.tm]
namespace import tpcccommon::*
set start [string first "proc neword " $source]
set end [string first "#RUN TPC-C" $source $start]
eval [string range $source $start [expr {$end-1}]]
proc gettimestamp {} { return 20200102030405 }
set lda [pg_connect -conninfo $conninfo]
pg_result [pg_exec $lda "set CLIENT_MIN_MESSAGES TO 'ERROR'"] -clear
pg_result [pg_exec $lda "select setseed(0.5)"] -clear
if {$procedures eq "false"} { fn_prep_statement $lda }
expr {srand(1)}
pg_select $lda "select max(w_id) from warehouse" row {set warehouses $row(max)}
set home [RandomNumber 1 $warehouses]
pg_select $lda "select max(d_id) from district" row {set districts $row(max)}
set stockDistrict [RandomNumber 1 $districts]
for {set i 0} {$i < $iterations} {incr i} {
    set choice [RandomNumber 1 23]
    if {$choice <= 10} {
        neword $lda $home $warehouses false false $procedures
    } elseif {$choice <= 20} {
        payment $lda $home $warehouses false false $procedures
    } elseif {$choice == 21} {
        delivery $lda $home false false $procedures
    } elseif {$choice == 22} {
        slev $lda $home $stockDistrict false false $procedures
    } else {
        ostat $lda $home false false $procedures
    }
}
pg_disconnect $lda
