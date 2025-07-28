package example

import future.keywords.if
import future.keywords.in

kilobyte := 1024

megabyte := 1024 * 1024

gigabyte := (1024 * 1024) * 1024

# Convert all numbers to humanized formats, like "3.9 Gb" or "140 Mb".
vmem_summary[k] := v if {
	some key, value in data.psutil.memory
	not endswith(key, "Percent")
	k := key
	v := humanize_number(value)
}

# Expose the percent usage value directly.
vmem_summary[k] := v if {
	some key, value in data.psutil.memory
	endswith(key, "Percent")
	k := key
	v := value
}

humanize_number(n) := v if {
	n < kilobyte
	v := concat(" ", [format_int(n, 10), "b"])
}

humanize_number(n) := v if {
	n >= kilobyte
	n < megabyte
	v := concat(" ", [sprintf("%v", [n / kilobyte]), "kb"])
}

humanize_number(n) := v if {
	n >= megabyte
	n < gigabyte
	v := concat(" ", [sprintf("%v", [n / megabyte]), "Mb"])
}

humanize_number(n) := v if {
	n >= gigabyte
	v := concat(" ", [sprintf("%v", [n / gigabyte]), "Gb"])
}
