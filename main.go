package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli"
	"io"
	"io/ioutil"
	"log"
)

var version string;

func main() {
	app := cli.NewApp()
	app.Version = version

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "input, i",
			Usage: "Read input from here.",
		},
		cli.StringFlag{
			Name:  "suffix, s",
			Usage: "Preseve old file contents the following suffix.",
		},
		cli.StringFlag{
			Name:  "skinny-fast",
			Usage: "Move the backup instead of copying. Momentarily unsafe.",
		},
		cli.BoolFlag{
			Name:  "memory, m",
			Usage: "Accumuate data in memory.",
		},
		cli.StringFlag{
			Name:  "tmp, t",
			Usage: "Put the tempfile in this drectory.  Must be on the same filesystem.",
		},
	}
	app.Action = SpongeAction

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type Backup interface {
	Begin() error
	Abort() error
	Complete() error
}

type SpongeFile interface {
	Begin() error
	Abort() error
	Write([]byte) error
	Complete() error
	Cleanup() error
}

func GetBackup(c *cli.Context) (Backup, error) {
	return &NoBackup{}, nil
}

func GetSpongeFile(c *cli.Context) (SpongeFile, error) {
	return NewMemorySponge(c.Args().First()), nil
}


func OpenInput(c *cli.Context) (*os.File, error) {
	inputFn := c.GlobalString("input")
	if inputFn == "" {
		return os.Stdin, nil
	}
	return os.Open(inputFn)
}

func SpongeAction(c *cli.Context) error {
	log.Print("Get backup.")
	bf, err := GetBackup(c)
	if err != nil {
		return err
	}
	log.Print("Get sponge.")
	sf, err := GetSpongeFile(c)
	if err != nil {
		return err
	}
	log.Print("Get input source.")
	in, err := OpenInput(c)
	if err != nil {
		return err
	}
	defer in.Close()
	log.Print("Begin backup.")
	if err := bf.Begin(); err != nil {
		return err;
	}
	log.Print("Beginning sponge.")
	if err := sf.Begin(); err != nil {
		bf.Abort();
		return err
	}
	defer func() {
		log.Print("Cleaning sponge.")
		sf.Cleanup()
	}()
	log.Print("Sponging data.")
	err = Transfer(os.Stdin, sf)
	if err != nil {
		log.Print("Sponging data failed. Aborting backup and sponge.")
		bf.Abort()
		sf.Abort()
		return err
	}
	log.Print("Completing backup.")
	if err := bf.Complete(); err != nil {
		log.Print("Backup completion failed. Aborting sponge.")
		sf.Abort()
		return err
	}
	log.Print("Completing sponge.")
	if err := sf.Complete(); err != nil {
		log.Print("Sponge completion failed.")
		return err
	}
	return nil
}

var READSIZE = 4096

func Transfer(in *os.File, sf SpongeFile) error {
	var err error = nil
	buf := make([]byte, READSIZE)
	for err == nil {
		n, err := in.Read(buf)
		log.Printf("Read and write %d bytes.", n)
		if n > 0 {
			sf.Write(buf[:n])
		}
		if n == 0 && err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return err
}

// Does no backup
type NoBackup struct {}

func (c *NoBackup) Begin() error {
	return nil
}

func (c *NoBackup) Abort() error {
	return nil
}

func (c *NoBackup) Complete() error {
	return nil
}


// Does no backup
type MemorySponge struct {
	TargetFn string
	Data     []byte
}

func NewMemorySponge(Target string) SpongeFile {
	return &MemorySponge{
		TargetFn: Target,
		Data: make([]byte, 0, READSIZE),
	}
}

func (ms *MemorySponge) Begin() error {
	return nil
}

func (ms *MemorySponge) Abort() error {
	return nil
}

func (ms *MemorySponge) Write(d []byte) error {
	log.Printf("Appending %d bytes to %d bytes in memory", len(d), len(ms.Data))
	ms.Data = append(ms.Data, d...)
	log.Printf("Total data length %d bytes in memory", len(ms.Data))
	return nil
}

func (ms *MemorySponge) Complete() error {
	fi, err := os.Stat(ms.TargetFn)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	mode := DEFAULT_MODE
	if err != nil {
		mode = fi.Mode()
	}
	log.Printf("Saving %d bytes to file %s with mode %o.", len(ms.Data), ms.TargetFn, mode)
	err = ioutil.WriteFile(ms.TargetFn, ms.Data, mode)
	if err != nil {
		return err
	}
	return nil
}

func (ms *MemorySponge) Cleanup() error {
	return nil
}


// Atomic Sponge
type AtomicSponge struct {
	SpongeFn   string
	TargetFn   string
	ActiveFile *os.File
	LeaveDirty bool
}

var DEFAULT_MODE os.FileMode = 0600

func (ms *AtomicSponge) Begin() error {
	f, err := os.OpenFile(ms.SpongeFn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, DEFAULT_MODE)
	if err != nil {
		return err
	}
	ms.ActiveFile = f
	return nil
}

func (ms *AtomicSponge) Abort() error {
	return nil
}

func (ms *AtomicSponge) Write(d []byte) error {
	n, err := ms.ActiveFile.Write(d)
	if err != nil {
		return err
	}
	if err == nil && n < len(d) {
		return io.ErrShortWrite
	}
	return nil
}

func (ms *AtomicSponge) Complete() error {
	err := ms.ActiveFile.Close()
	ms.ActiveFile = nil
	if err != nil {
		return err
	}
	fi, err := os.Stat(ms.TargetFn)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		os.Chmod(ms.TargetFn, fi.Mode())
	}
	if err := os.Rename(ms.SpongeFn, ms.TargetFn); err != nil {
		return err
	}
	return nil
}

func (ms *AtomicSponge) Cleanup() error {
	if ms.LeaveDirty {
		return nil
	}
	if _, err := os.Stat(ms.SpongeFn); os.IsNotExist(err) {
		return nil
	}
	if err := os.Remove(ms.SpongeFn); err != nil {
		return err
	}
	return nil
}
