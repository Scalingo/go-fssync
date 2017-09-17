package main

import (
	"flag"
	"log"

	"github.com/Scalingo/go-fssync"
)

func main() {
	withCheckum := flag.Bool("checksum", false, "compare files with checksum")
	preserveOwnership := flag.Bool("preserve-ownership", false, "preservice ownership of source")

	flag.Parse()

	options := []func(s *fssync.FsSyncer){}
	if *withCheckum {
		options = append(options, fssync.WithChecksum)
	}
	if *preserveOwnership {
		options = append(options, fssync.PreserveOwnership)
	}
	syncer := fssync.New(options...)

	args := flag.Args()
	if len(args) != 2 {
		log.Fatalln("Usage: ./fssync [options] <src> <dst>")
	}
	src := args[0]
	dst := args[1]
	_, err := syncer.Sync(src, dst)
	if err != nil {
		log.Fatalln(err)
	}
}
