package main

import (
	workload "github.com/stroppy-io/stroppy-contrib/go-tpc-tpcc/workload"
	stroppy "github.com/stroppy-io/stroppy/v6"
)

func main() { stroppy.Main(workload.Test) }
