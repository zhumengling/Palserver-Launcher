//go:build linux

package main

import "os"

func copyTreeEntryIsReparsePoint(os.FileInfo) bool { return false }
