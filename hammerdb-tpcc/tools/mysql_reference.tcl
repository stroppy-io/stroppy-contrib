#!/usr/bin/env tclsh
# Use the pinned HammerDB loader with mysqltcl, without loading its GUI.
package require mysqltcl
package require Thread
set root [lindex $argv 0]
set host [lindex $argv 1]
set port [lindex $argv 2]
set user [lindex $argv 3]
set password [lindex $argv 4]
set database [lindex $argv 5]
source [file join $root modules tpcccommon-1.0.tm]
namespace import tpcccommon::*
set channel [open [file join $root src mysql mysqloltp.tcl] r]
set text [read $channel]
close $channel
set start [string first "proc CreateStoredProcs " $text]
set end [string first "proc ConnectToMySQL " $text]
eval [string range $text $start [expr {$end-1}]]
set start [string first "proc CreateTables " $text]
set end [string first "proc chk_socket " $text]
eval [string range $text $start [expr {$end-1}]]
set connection [mysqlconnect -host $host -port $port -user $user -password $password]
mysqlexec $connection "CREATE DATABASE `$database` CHARACTER SET latin1 COLLATE latin1_swedish_ci"
mysqluse $connection $database
mysql::autocommit $connection 0
expr {srand(1)}
CreateTables $connection innodb 0 false
LoadItems $connection 100000
LoadWare $connection 1 1 100000 10
LoadCust $connection 1 1 3000 10
LoadOrd $connection 1 1 100000 3000 10
CreateStoredProcs $connection
mysqlclose $connection
