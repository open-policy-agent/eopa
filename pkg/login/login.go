package login

import (
	"context"
	"fmt"
	"os"
)

func Start(ctx context.Context, url string) error {
	fmt.Fprintf(os.Stderr, "would be connecting to %s\n", url)
	return ctx.Err()
}
