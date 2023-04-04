package utils

// Contains returns true if input contains the given string.
func Contains(input []string, s string) bool {
        for _, k := range input {
                if k == s {
                        return true
                }
        }
        return false
}

