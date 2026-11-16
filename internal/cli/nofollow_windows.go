//go:build windows

package cli

// openNoFollow: Windows has no O_NOFOLLOW; writeInCheckout's Lstat walk is the guard there.
const openNoFollow = 0
