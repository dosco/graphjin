package sdata

type funcInfo struct {
	name, desc, ftype string
}

var funcList = []funcInfo{
	{name: "count", desc: "Count the number of rows", ftype: "bigint"},
	{name: "sum", desc: "Calculate the sum", ftype: "bigint"},
	{name: "avg", desc: "Calculate the average", ftype: "decimal"},
	{name: "max", desc: "Find the maximum value", ftype: "decimal"},
	{name: "min", desc: "Find the minimum value", ftype: "decimal"},
	{name: "stddev", desc: "Calculate the standard deviation", ftype: "decimal"},
	{name: "stddev_pop", desc: "Calculate the population standard deviation", ftype: "decimal"},
	{name: "stddev_samp", desc: "Calculate the sample standard deviation", ftype: "decimal"},
	{name: "var_samp", desc: "Calculate the sample variance", ftype: "decimal"},
	{name: "var_pop", desc: "Calculate the population sample variance", ftype: "decimal"},
	{name: "length", desc: "Calculate the length", ftype: "decimal"},
	{name: "lower", desc: "Convert to lowercase", ftype: "decimal"},
	{name: "upper", desc: "Convert to uppercase", ftype: "decimal"},
}

// maybe add
// "array_agg",
// "json_agg",
// "unnest",
