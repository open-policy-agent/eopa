package example

import rego.v1

# Convert all numbers to humanized formats, like "3.9 Gb" or "140 Mb".
vmem_summary[k] := v if {
	some k, value in data.psutil.memory
	not endswith(k, "Percent")
	v := humanize_number(value)
}

# Expose the percent usage value directly.
vmem_summary[k] := v if {
	some k, v in data.psutil.memory
	endswith(k, "Percent")
}

humanize_number(n) := v if {
	kilobyte := 1024
	n < kilobyte
	v := concat(" ", [format_int(n, 10), "b"])
}

humanize_number(n) := v if {
	kilobyte := 1024
	n >= kilobyte
	megabyte := 1024 * 1024
	n < megabyte
	v := concat(" ", [sprintf("%v", [n / kilobyte]), "kb"])
}

humanize_number(n) := v if {
	megabyte := 1024 * 1024
	n >= megabyte
	gigabyte := 1024 * 1024 * 1024
	n < gigabyte
	v := concat(" ", [sprintf("%v", [n / megabyte]), "Mb"])
}

humanize_number(n) := v if {
	gigabyte := 1024 * 1024 * 1024
	n >= gigabyte
	v := concat(" ", [sprintf("%v", [n / gigabyte]), "Gb"])
}
