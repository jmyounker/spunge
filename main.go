package main

import (
	"fmt"
	"os"
	"io"
	"io/ioutil"
	"log"
	"errors"
	"path"
	"strings"

	"github.com/urfave/cli"
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
			Name:  "backup, b",
			Usage: "Backs up to the specified file.",
		},
		cli.BoolFlag{
			Name:  "atomic",
			Usage: "Write atomicly. Only needed with --memory.",
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
			Name:  "tmpdir, t",
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
	if c.GlobalString("backup") == "" {
		log.Print("Choosing no backup.")
		return &NoBackup{}, nil
	}
	log.Print("Choosing concurrent backup.")
	return NewConcurrentBackup(c.Args().First(), c.GlobalString("backup")), nil
}

func GetSpongeFile(c *cli.Context) (SpongeFile, error) {
	if !c.GlobalBool("memory") {
		return NewAtomicSponge(
			c.Args().First(),
			c.GlobalString("tmpdir"),
			c.GlobalBool("leave-dirty")),
			nil
	}
	if c.GlobalBool("atomic") {
		return NewAtomicMemorySponge(
			c.Args().First(),
			c.GlobalString("tmpdir"),
			c.GlobalBool("leave-dirty")),
			nil
	}
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
	if c.GlobalBool("atomic") && !c.GlobalBool("memory") {
		return errors.New("--atomic makes no sense wihout --memory")
	}
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


type ConcurrentBackup struct {
	SourceFn string
	BackupFn string
	Done chan error
}

func NewConcurrentBackup(source, backup string) Backup {
	return &ConcurrentBackup{
		SourceFn: source,
		BackupFn: BackupFile(backup, source),
		Done: make(chan error),
	}
}

func (cb *ConcurrentBackup) Begin() error {
	log.Printf("Backing up %s to %s", cb.SourceFn, cb.BackupFn)
	source, err := os.Open(cb.SourceFn)
	if err != nil {
		close(cb.Done)
		return err
	}

	backup, err := os.OpenFile(cb.BackupFn, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		source.Close()
		close(cb.Done)
		return err
	}
	go DoBackup(source, backup, cb.Done)
	return nil
}

func DoBackup(source, dest *os.File, done chan error) {
	log.Printf("Starting background copy.")
	defer source.Close()
	defer dest.Close()
	n, err := io.Copy(dest, source)
	log.Printf("Backed up %d bytes", n)
	if err != nil {
		done <- err
	}
	close(done)
}

func (cb *ConcurrentBackup) Abort() error {
	log.Print("Waiting for backup to complete.")
	return <- cb.Done
}

func (cb *ConcurrentBackup) Complete() error {
	log.Print("Waiting for backup to complete.")
	err := <- cb.Done
	if err != nil {
		return err
	}
	fi, err := os.Stat(cb.SourceFn)
	if err != nil {
		log.Printf("Stat failed. Not replicating permissions.")
		return err
	}
	log.Printf("Updating permissions on %s to %o", cb.BackupFn, fi.Mode())
	return os.Chmod(cb.BackupFn, fi.Mode())
}

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
	TempDir    string
	TargetFn   string
	Sponge     *os.File
	LeaveDirty bool
}

var DEFAULT_MODE os.FileMode = 0600

func TempDir(tempDir, targetFn string) string {
	if tempDir == "" {
		return path.Dir(targetFn)
	}
	tempDir = strings.Replace(tempDir, "{dir}", path.Dir(targetFn), -1)
	return strings.Replace(tempDir, "{base}", path.Base(targetFn), -1)
}

func BackupFile(backupFile, targetFn string) string {
	backupFile = strings.Replace(backupFile, "{dir}", path.Dir(targetFn), -1)
	backupFile = strings.Replace(backupFile, "{base}", path.Base(targetFn), -1)
	return strings.Replace(backupFile, "{file}", targetFn, -1)
}

func NewAtomicSponge(targetFn, tempDir string, leaveDirty bool) SpongeFile {
	return &AtomicSponge{
		TargetFn: targetFn,
		TempDir: TempDir(tempDir, targetFn),
		LeaveDirty: leaveDirty,
	}
}

func (ms *AtomicSponge) Begin() error {
	sponge, err := ioutil.TempFile(ms.TempDir, ".sponge")
	if err != nil {
		log.Printf("Cannot create sponge file in %s", ms.TempDir)
		return err
	}
	ms.Sponge = sponge
	ms.SpongeFn = sponge.Name()
	log.Printf("Create sponge file %s", sponge.Name())
	return nil
}

func (ms *AtomicSponge) Abort() error {
	return nil
}

func (ms *AtomicSponge) Write(d []byte) error {
	n, err := ms.Sponge.Write(d)
	log.Printf("Wrote %d bytes to sponge file.", n)
	if err != nil {
		return err
	}
	if err == nil && n < len(d) {
		return io.ErrShortWrite
	}
	return nil
}

func (ms *AtomicSponge) Complete() error {
	log.Print("Closing sponge.")
	err := ms.Sponge.Close()
	ms.Sponge = nil
	if err != nil {
		return err
	}
	fi, err := os.Stat(ms.TargetFn)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		log.Printf("Setting sponge permissions to match existing file: %o.", fi.Mode())
		if err := os.Chmod(ms.SpongeFn, fi.Mode()); err != nil {
			log.Printf("Cannot set %s to mode %o", ms.SpongeFn, fi.Mode())
		}

	}
	log.Printf("Renaming sponge file %s to %s", ms.SpongeFn, ms.TargetFn)
	if err := os.Rename(ms.SpongeFn, ms.TargetFn); err != nil {
		return err
	}
	return nil
}

func (ms *AtomicSponge) Cleanup() error {
	if ms.LeaveDirty {
		log.Print("Leaving dirty environment.")
		return nil
	}
	if _, err := os.Stat(ms.SpongeFn); os.IsNotExist(err) {
		log.Print("Nothing to clean.")
		return nil
	}
	log.Printf("Removing stray sponge file %s.", ms.SpongeFn)
	if err := os.Remove(ms.SpongeFn); err != nil {
		return err
	}
	return nil
}


type AtomicMemorySponge struct {
	Writer SpongeFile
	Data []byte
}

func NewAtomicMemorySponge(targetFn, tmpDir string, leaveDirty bool) SpongeFile {
	return &AtomicMemorySponge{
		Writer: NewAtomicSponge(targetFn, tmpDir, leaveDirty),
		Data: make([]byte, 0, READSIZE),
	}
}

func (ams *AtomicMemorySponge) Begin() error {
	return nil
}

func (ams *AtomicMemorySponge) Write(d []byte) error {
	log.Printf("Appending %d bytes to %d bytes in memory", len(d), len(ams.Data))
	ams.Data = append(ams.Data, d...)
	log.Printf("Total data length %d bytes in memory", len(ams.Data))
	return nil
}

func (ams *AtomicMemorySponge) Abort() error {
	return ams.Writer.Abort()
}

func (ams *AtomicMemorySponge) Complete() error {
	if err := ams.Writer.Begin(); err != nil {
		return err
	}
	if err := ams.Writer.Write(ams.Data); err != nil {
		return err
	}
	return ams.Writer.Complete()
}

func (ams *AtomicMemorySponge) Cleanup() error {
	return ams.Writer.Cleanup()
}