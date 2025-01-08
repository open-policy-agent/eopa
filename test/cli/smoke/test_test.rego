package test

test_result if {
    result with input as {"yay":"bar"}
}

test_not_result if {
    not result with input as {"yay":"yay"}
}
