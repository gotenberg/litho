package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// corpusImageRepository is the repository of the image provisioning builds
// and runs; its tag is the content it is built from ([corpusImageTag]).
const corpusImageRepository = "litho-corpus-builder"

// corpusImageTagLength is how many hexadecimal digits of the content digest
// the tag keeps: 16, 64 bits, far past any chance of two provisioning trees
// meeting on one tag.
const corpusImageTagLength = 16

// corpusImage returns the image provisioning builds and runs for the corpus
// directory of this tree at the resolved pins: [corpusImageRepository]
// tagged by [corpusImageTag]. Two trees whose Dockerfile, copied files or
// pins differ name two images, and a tree whose image exists finds it
// built.
func corpusImage() (string, error) {
	root := corpusRoot()
	if root == "" {
		return "", errors.New("no corpus directory found")
	}
	pins, err := loadPins()
	if err != nil {
		return "", err
	}
	tag, err := corpusImageTag(root, pins.buildArgs())
	if err != nil {
		return "", err
	}
	return corpusImageRepository + ":" + tag, nil
}

// corpusImageTag returns the tag of the image built from the build context
// dir with buildArgs: the first [corpusImageTagLength] hexadecimal digits
// of the SHA-256 of the Dockerfile, of every file of dir its COPY and ADD
// instructions name, by path and SHA-256 in path order, and of the build
// arguments in order. A source that matches no file is an error, as docker
// build fails on it.
func corpusImageTag(dir string, buildArgs []string) (string, error) {
	dockerfile, err := os.ReadFile(filepath.Join(dir, "Dockerfile")) //nolint:gosec // the corpus directory's Dockerfile
	if err != nil {
		return "", err
	}
	sources, err := dockerfileSources(dockerfile)
	if err != nil {
		return "", err
	}
	files := map[string]bool{}
	for _, src := range sources {
		matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(src)))
		if err != nil || len(matches) == 0 {
			return "", fmt.Errorf("the Dockerfile copies %s, which matches no file of %s", src, dir)
		}
		for _, m := range matches {
			err := filepath.WalkDir(m, func(p string, d fs.DirEntry, err error) error { //nolint:gosec // m is a glob match inside the corpus directory, from its own Dockerfile
				if err != nil || d.IsDir() {
					return err
				}
				rel, err := filepath.Rel(dir, p)
				files[filepath.ToSlash(rel)] = true
				return err
			})
			if err != nil {
				return "", err
			}
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	sum := sha256.Sum256(dockerfile)
	fmt.Fprintf(h, "Dockerfile\x00%x\n", sum)
	for _, n := range names {
		digest, err := fileDigest(filepath.Join(dir, filepath.FromSlash(n)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%s\n", n, digest)
	}
	for _, a := range buildArgs {
		fmt.Fprintf(h, "arg\x00%s\n", a)
	}
	return hex.EncodeToString(h.Sum(nil))[:corpusImageTagLength], nil
}

// dockerfileSources returns the sources of the COPY and ADD instructions of
// a Dockerfile that read the build context, as written: every argument but
// the last, the destination, and the flags. An instruction with --from
// copies out of another stage or image and reads no file of the context.
func dockerfileSources(dockerfile []byte) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(dockerfile))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || (!strings.EqualFold(fields[0], "COPY") && !strings.EqualFold(fields[0], "ADD")) {
			continue
		}
		args := fields[1:]
		from := false
		for len(args) > 0 && strings.HasPrefix(args[0], "--") {
			from = from || strings.HasPrefix(args[0], "--from=")
			args = args[1:]
		}
		if from {
			continue
		}
		if len(args) < 2 || strings.HasPrefix(args[0], "[") || strings.HasSuffix(sc.Text(), `\`) {
			return nil, fmt.Errorf("the Dockerfile instruction %q: want sources and a destination on one line", sc.Text())
		}
		out = append(out, args[:len(args)-1]...)
	}
	return out, sc.Err()
}
