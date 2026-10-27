#!/usr/bin/env tclsh
# Arguments: checkout host port user password database text|prepared|client iterations.
package require mysqltcl
package require Thread
set root [lindex $argv 0]
lassign [lrange $argv 1 5] host port user password database
set profile [lindex $argv 6]
set iterations [lindex $argv 7]
source [file join $root modules tpcccommon-1.0.tm]
namespace import tpcccommon::*
set channel [open [file join $root src mysql mysqloltp.tcl] r]
set text [read $channel]
close $channel
set start [string first "proc neword " $text [string first "proc loadmysqltpcc" $text]]
set end [string first "#RUN TPC-C" $text $start]
eval [string range $text $start [expr {$end-1}]]
if {$profile eq "client"} {
    set start [string first "set newordbody \{" $text]
    set end [string first "if \{ \$testtype eq \"test\" \}" $text $start]
    eval [string range $text $start [expr {$end-1}]]
    proc neword {mysql_handler no_w_id w_id_input prepare RAISEERROR} $newordbody
    proc payment {mysql_handler p_w_id w_id_input prepare RAISEERROR} $paybody
    proc delivery {mysql_handler w_id prepare RAISEERROR} $delivbody
    proc slev {mysql_handler w_id stock_level_d_id prepare RAISEERROR} $stockbody
    proc ostat {mysql_handler w_id prepare RAISEERROR} $ostatbody
}
proc gettimestamp {} { return 20200102030405 }
set connection [mysqlconnect -host $host -port $port -user $user -password $password]
mysqluse $connection $database
mysql::autocommit $connection 0
mysqlexec $connection "set session rand_seed1=123,rand_seed2=456"
set prepare [expr {$profile eq "prepared"}]
if {$prepare} { foreach statement {neword_st payment_st delivery_st slev_st ostat_st} { prep_statement $connection $statement } }
expr {srand(1)}
set warehouses [mysql::sel $connection "select max(w_id) from warehouse" -flatlist]
set home [RandomNumber 1 $warehouses]
set districts [mysql::sel $connection "select max(d_id) from district" -flatlist]
set stockDistrict [RandomNumber 1 $districts]
for {set i 0} {$i < $iterations} {incr i} {
    set choice [RandomNumber 1 23]
    if {$choice <= 10} {
        neword $connection $home $warehouses $prepare false
    } elseif {$choice <= 20} {
        payment $connection $home $warehouses $prepare false
    } elseif {$choice == 21} {
        delivery $connection $home $prepare false
    } elseif {$choice == 22} {
        slev $connection $home $stockDistrict $prepare false
    } else {
        ostat $connection $home $prepare false
    }
}
mysqlclose $connection
