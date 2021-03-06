package main

import (
	"fmt"
	"os"
	"io"
	"io/ioutil"
	"errors"
	"path"
	"strings"

	"github.com/urfave/cli"
)

var version string;

var READSIZE = 4096

func main() {
	app := cli.NewApp()
	app.Usage = "Accumulate data and write to storage when complete."
	app.Version = version

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "input, i",
			Usage: "Read input from here.  Exists for testing.",
		},
		cli.StringFlag{
			Name:  "backup, b",
			Usage: "Backs up target to the specified file.",
		},
		cli.BoolFlag{
			Name:  "atomic, a",
			Usage: "Write atomicly. Only needed with --memory.",
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

func SpongeAction(c *cli.Context) error {
	if len(c.Args()) == 0 {
		return errors.New("Destination file required.")
	}
	if len(c.Args()) > 1 {
		return errors.New("Can only sponge to one destination.")
	}
	if c.GlobalBool("atomic") && !c.GlobalBool("memory") {
		return errors.New("--atomic makes no sense wihout --memory")
	}
	bf, err := GetBackup(c)
	if err != nil {
		return err
	}
	sf, err := GetSpongeFile(c)
	if err != nil {
		return err
	}
	in, err := OpenInput(c)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := bf.Begin(); err != nil {
		return err;
	}
	if err := sf.Begin(); err != nil {
		bf.Abort();
		return err
	}
	defer func() {
		sf.Cleanup()
	}()
	err = Transfer(os.Stdin, sf)
	if err != nil {
		bf.Abort()
		sf.Abort()
		return err
	}
	if err := bf.Complete(); err != nil {
		sf.Abort()
		return err
	}
	if err := sf.Complete(); err != nil {
		return err
	}
	return nil
}

func Transfer(in *os.File, sf SpongeFile) error {
	var err error = nil
	buf := make([]byte, READSIZE)
	for err == nil {
		n, err := in.Read(buf)
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

func OpenInput(c *cli.Context) (*os.File, error) {
	inputFn := c.GlobalString("input")
	if inputFn == "" {
		return os.Stdin, nil
	}
	return os.Open(inputFn)
}

// Backups perform backups of the original file.

type Backup interface {
	Begin() error
	Abort() error
	Complete() error
}

func GetBackup(c *cli.Context) (Backup, error) {
	if c.GlobalString("backup") == "" {
		return &NoBackup{}, nil
	}
	return NewConcurrentBackup(c.Args().First(), c.GlobalString("backup")), nil
}

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
		Done: nil,
	}
}

func (cb *ConcurrentBackup) Begin() error {
	done, err := Copy(cb.SourceFn, cb.BackupFn)
	if err != nil {
		return err
	}
	cb.Done = done
	return nil
}

func (cb *ConcurrentBackup) Abort() error {
	if cb.Done == nil {
		return nil
	}
	return <- cb.Done
}

func (cb *ConcurrentBackup) Complete() error {
	if cb.Done == nil {
		return nil
	}
	err := <- cb.Done
	if err != nil {
		return err
	}
	fi, err := os.Stat(cb.SourceFn)
	if err != nil {
		return err
	}
	return os.Chmod(cb.BackupFn, fi.Mode())
}

// Sponges accumulate data before moving them into the correct location on
// the filesystem

type SpongeFile interface {
	Begin() error
	Abort() error
	Write([]byte) error
	Complete() error
	Cleanup() error
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
	ms.Data = append(ms.Data, d...)
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
	err = ioutil.WriteFile(ms.TargetFn, ms.Data, mode)
	if err != nil {
		return err
	}
	return nil
}

func (ms *MemorySponge) Cleanup() error {
	return nil
}

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
		return err
	}
	ms.Sponge = sponge
	ms.SpongeFn = sponge.Name()
	return nil
}

func (ms *AtomicSponge) Abort() error {
	return nil
}

func (ms *AtomicSponge) Write(d []byte) error {
	n, err := ms.Sponge.Write(d)
	if err != nil {
		return err
	}
	if err == nil && n < len(d) {
		return io.ErrShortWrite
	}
	return nil
}

func (ms *AtomicSponge) Complete() error {
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
		if err := os.Chmod(ms.SpongeFn, fi.Mode()); err != nil {
		}

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
	ams.Data = append(ams.Data, d...)
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

func Copy(src, dest string) (chan error, error) {
	if src == dest {
		return nil, errors.New("Will not copy to same filename.")
	}
	sfi, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		} else {
			return nil, err
		}
	}
	if !sfi.Mode().IsRegular() {
		return nil, fmt.Errorf("Cannot copy non-regular source file %s (%q)", src, sfi.Mode().String())
	}
	dfi, err := os.Stat(dest)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && !dfi.Mode().IsRegular() {
		return nil, fmt.Errorf("Cannot copy to non-regular destination %s (%q)", dest, dfi.Mode().String())
	}
	if os.SameFile(sfi, dfi) {
		return nil, nil
	}
	if err = os.Link(src, dest); err == nil {
		return nil, nil
	}
	source, err := os.Open(src)
	if err != nil {
		return nil, err
	}

	backup, err := os.Create(dest)
	if err != nil {
		source.Close()
		return nil, err
	}
	done := make(chan error)
	go DoConcurrentCopy(source, backup, done)
	return done, nil
}

func DoConcurrentCopy(source, dest *os.File, done chan error) {
	defer source.Close()
	defer dest.Close()
	_, err := io.Copy(dest, source)
	if err != nil {
		done <- err
	}
	close(done)
}
