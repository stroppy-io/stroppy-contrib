#!/usr/bin/env tclsh
# Execute the pinned HammerDB PostgreSQL builder without the HammerDB GUI.
# Arguments: reference checkout, libpq connection string, functions|procedures.
package require Pgtcl
package require Thread
set root [lindex $argv 0]
set conninfo [lindex $argv 1]
set procedures [expr {[lindex $argv 2] eq "procedures" ? "true" : "false"}]
set channel [open [file join $root src postgresql pgoltp.tcl] r]
set source [read $channel]
close $channel
source [file join $root modules tpcccommon-1.0.tm]
namespace import tpcccommon::*
set start [string first "proc CreateStoredProcs " $source]
set end [string first "proc GatherStatistics " $source]
eval [string range $source $start [expr {$end-1}]]
set start [string first "proc CreateTables " $source]
set end [string first "proc do_tpcc " $source]
eval [string range $source $start [expr {$end-1}]]
set lda [pg_connect -conninfo $conninfo]
pg_result [pg_exec $lda "set CLIENT_MIN_MESSAGES TO 'ERROR'"] -clear
expr {srand(1)}
CreateTables $lda false false 0
LoadItems $lda 100000
LoadWare $lda 1 1 100000 10
LoadCust $lda 1 1 3000 10 false
LoadOrd $lda 1 1 100000 3000 10 false
CreateIndexes $lda
CreateStoredProcs $lda false false $procedures
pg_result [pg_exec $lda ANALYZE] -clear
pg_disconnect $lda
