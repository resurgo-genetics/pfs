package btrfs

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pachyderm/pachyderm/src/log"
)

var run_string string

// writeFile quickly writes a string to disk.
func writeFile(name, content string, t *testing.T) {
	f, err := Create(name)
	check(err, t)
	f.WriteString(content + "\n")
	check(f.Close(), t)
}

var suffix int = 0

// writeLots writes a lots of data to disk in 128 MB files
func writeLots(prefix string, nFiles int, t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()
	for i := 0; i < nFiles; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f, err := Create(fmt.Sprintf("%s-%d", prefix, suffix))
			check(err, t)
			suffix++
			_, err = io.Copy(f, io.LimitReader(rand.Reader, (1<<20)*16))
			check(err, t)
			check(f.Close(), t)
		}(i)
	}
}

func removeFile(name string, t *testing.T) {
	err := Remove(name)
	if err != nil {
		t.Fatal(err)
	}
}

// TestOsOps checks that reading, writing, and deletion are correct on BTRFS.
func TestOsOps(t *testing.T) {
	writeFile("foo", "foo", t)
	CheckFile("foo", "foo", t)
	removeFile("foo", t)
	CheckNoExists("foo", t)
}

// TestGit checks that the Git-style interface to BTRFS is correct.
func TestGit(t *testing.T) {
	srcRepo := "repo_TestGit"
	// Create the repo:
	check(Init(srcRepo), t)

	// Write a file "file" and create a commit "commit1":
	writeFile(fmt.Sprintf("%s/master/file", srcRepo), "foo", t)
	if !testing.Short() {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 3, t)
	}
	err := Commit(srcRepo, "commit1", "master")
	check(err, t)
	CheckFile(path.Join(srcRepo, "commit1", "file"), "foo", t)

	// Create a new branch "branch" from commit "commit1", and check that
	// it contains the file "file":
	check(Branch(srcRepo, "commit1", "branch"), t)
	CheckFile(fmt.Sprintf("%s/branch/file", srcRepo), "foo", t)

	// Create a file "file2" in branch "branch", and commit it to
	// "commit2":
	writeFile(fmt.Sprintf("%s/branch/file2", srcRepo), "foo", t)
	if !testing.Short() {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 3, t)
	}
	err = Commit(srcRepo, "commit2", "branch")
	check(err, t)
	CheckFile(path.Join(srcRepo, "commit2", "file2"), "foo", t)

	// Print BTRFS hierarchy data for humans:
	check(_log(srcRepo, "", Desc, func(r io.Reader) error {
		_, err := io.Copy(os.Stdout, r)
		return err
	}), t)
}

func TestNewRepoIsEmpty(t *testing.T) {
	srcRepo := "repo_TestNewRepoIsEmpty"
	check(Init(srcRepo), t)

	// ('master' is the default branch)
	dirpath := path.Join(srcRepo, "master")
	descriptors, err := ReadDir(dirpath)
	check(err, t)
	if len(descriptors) != 1 || descriptors[0].Name() != ".meta" {
		t.Fatalf("expected empty repo")
	}
}

func TestCommitsAreReadOnly(t *testing.T) {
	srcRepo := "repo_TestCommitsAreReadOnly"
	check(Init(srcRepo), t)

	err := Commit(srcRepo, "commit1", "master")
	check(err, t)

	_, err = Create(fmt.Sprintf("%s/commit1/file", srcRepo))
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "read-only file system") {
		t.Fatalf("expected read-only filesystem error, got %s", err)
	}
}

func TestBranchesAreReadWrite(t *testing.T) {
	srcRepo := "repo_TestBranchesAreReadWrite"
	check(Init(srcRepo), t)

	// Make a commit
	check(Commit(srcRepo, "my_commit", "master"), t)

	// Make a branch
	check(Branch(srcRepo, "my_commit", "my_branch"), t)

	fn := fmt.Sprintf("%s/my_branch/file", srcRepo)
	writeFile(fn, "some content", t)
	CheckFile(fn, "some content", t)
}

// TestReplication checks that replication is correct when using local BTRFS.
// Uses `Pull`
// This is heavier and hairier, do it last.
func TestReplication(t *testing.T) {
	t.Skip("implement this")
}

// TestSendRecv checks the Send and Recv replication primitives.
func TestSendRecv(t *testing.T) {
	// Create a source repo:
	srcRepo := "repo_TestSendRecv_src"
	check(Init(srcRepo), t)

	// Create a file in the source repo:
	writeFile(fmt.Sprintf("%s/master/myfile1", srcRepo), "foo", t)
	if testing.Short() {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 1, t)
	} else {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 10, t)
	}

	// Create a commit in the source repo:
	check(Commit(srcRepo, "mycommit1", "master"), t)

	// Create another file in the source repo:
	writeFile(fmt.Sprintf("%s/master/myfile2", srcRepo), "bar", t)
	if testing.Short() {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 1, t)
	} else {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 10, t)
	}

	// Create a another commit in the source repo:
	check(Commit(srcRepo, "mycommit2", "master"), t)

	// Create a destination repo:
	dstRepo := "repo_TestSendRecv_dst"
	check(Init(dstRepo), t)
	repo2Recv := func(r io.Reader) error { return recv(dstRepo, r) }

	// Verify that the commits "mycommit1" and "mycommit2" do not exist in destination:
	CheckNoExists(fmt.Sprintf("%s/master/mycommit1", dstRepo), t)
	CheckNoExists(fmt.Sprintf("%s/master/mycommit2", dstRepo), t)

	// Run a Send/Recv operation to fetch data from the older "mycommit1".
	// This verifies that tree copying works:
	check(send(srcRepo, "mycommit1", repo2Recv), t)

	// Check that the file from mycommit1 exists, but not from mycommit2:
	CheckFile(fmt.Sprintf("%s/mycommit1/myfile1", dstRepo), "foo", t)
	CheckNoExists(fmt.Sprintf("%s/mycommit2/myfile2", dstRepo), t)

	// Send again, this time starting from mycommit1 and going to mycommit2:
	check(send(srcRepo, "mycommit2", repo2Recv), t)

	// Verify that files from both commits are present:
	CheckFile(fmt.Sprintf("%s/mycommit1/myfile1", dstRepo), "foo", t)
	CheckFile(fmt.Sprintf("%s/mycommit2/myfile2", dstRepo), "bar", t)
}

// TestBranchesAreNotReplicated // this is a known property, but not desirable long term
// TestCommitsAreReplicated // Uses Send and Recv
func TestCommitsAreReplicated(t *testing.T) {
	// Create a source repo:
	srcRepo := "repo_TestCommitsAreReplicated_src"
	check(Init(srcRepo), t)

	// Create a file in the source repo:
	writeFile(fmt.Sprintf("%s/master/myfile1", srcRepo), "foo", t)
	if testing.Short() {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 1, t)
	} else {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 10, t)
	}

	// Create a commit in the source repo:
	check(Commit(srcRepo, "mycommit1", "master"), t)

	// Create another file in the source repo:
	writeFile(fmt.Sprintf("%s/master/myfile2", srcRepo), "bar", t)
	if testing.Short() {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 1, t)
	} else {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 10, t)
	}

	// Create a another commit in the source repo:
	check(Commit(srcRepo, "mycommit2", "master"), t)

	// Create a destination repo:
	dstRepo := "repo_TestCommitsAreReplicated_dst"
	check(Init(dstRepo), t)

	// Verify that the commits "mycommit1" and "mycommit2" do exist in source:
	CheckFile(fmt.Sprintf("%s/mycommit1/myfile1", srcRepo), "foo", t)
	CheckFile(fmt.Sprintf("%s/mycommit2/myfile2", srcRepo), "bar", t)

	// Verify that the commits "mycommit1" and "mycommit2" do not exist in destination:
	CheckNoExists(fmt.Sprintf("%s/mycommit1", dstRepo), t)
	CheckNoExists(fmt.Sprintf("%s/mycommit2", dstRepo), t)

	// Run a Pull/Recv operation to fetch all commits:
	err := Pull(srcRepo, "", NewLocalReplica(dstRepo))
	check(err, t)

	// Verify that files from both commits are present:
	CheckFile(fmt.Sprintf("%s/mycommit1/myfile1", dstRepo), "foo", t)
	CheckFile(fmt.Sprintf("%s/mycommit2/myfile2", dstRepo), "bar", t)

	// Now check that we can use dstRepo as the source for replication
	// Create a second dest repo:
	dstRepo2 := "repo_TestCommitsAreReplicated_dst2"
	check(Init(dstRepo2), t)

	// Verify that the commits "mycommit1" and "mycommit2" do not exist in destination:
	CheckNoExists(fmt.Sprintf("%s/mycommit1", dstRepo2), t)
	CheckNoExists(fmt.Sprintf("%s/mycommit2", dstRepo2), t)

	// Run a Pull/Recv operation to fetch all commits:
	err = Pull(dstRepo, "", NewLocalReplica(dstRepo2))
	check(err, t)

	// Verify that files from both commits are present:
	CheckFile(fmt.Sprintf("%s/mycommit1/myfile1", dstRepo2), "foo", t)
	CheckFile(fmt.Sprintf("%s/mycommit2/myfile2", dstRepo2), "bar", t)
	CheckFile(fmt.Sprintf("%s/master/myfile1", dstRepo), "foo", t)
	CheckFile(fmt.Sprintf("%s/master/myfile2", dstRepo), "bar", t)
}

// TestSendWithMissingIntermediateCommitIsCorrect(?) // ? means we don't know what the behavior is.
func TestSendWithMissingIntermediateCommitIsCorrect(t *testing.T) {
	//FIXME: https://github.com/pachyderm/pachyderm/issues/60
	t.Skip("Removing commits currently breaks replication, this is ok for now because users can't remove commits.")
	// Create a source repo:
	srcRepo := "repo_TestSendWithMissingIntermediateCommitIsCorrect_src"
	check(Init(srcRepo), t)

	// Create a file in the source repo:
	writeFile(fmt.Sprintf("%s/master/myfile1", srcRepo), "foo", t)

	// Create a commit in the source repo:
	check(Commit(srcRepo, "mycommit1", "master"), t)

	// Create another file in the source repo:
	writeFile(fmt.Sprintf("%s/master/myfile2", srcRepo), "bar", t)

	// Create a another commit in the source repo:
	check(Commit(srcRepo, "mycommit2", "master"), t)

	// Delete intermediate commit "mycommit1":
	check(subvolumeDelete(fmt.Sprintf("%s/mycommit1", srcRepo)), t)

	// Verify that the commit "mycommit1" does not exist and "mycommit2" does in the source repo:
	CheckNoExists(fmt.Sprintf("%s/mycommit1", srcRepo), t)
	CheckFile(fmt.Sprintf("%s/mycommit2/myfile2", srcRepo), "bar", t)

	// Create a destination repo:
	dstRepo := "repo_TestSendWithMissingIntermediateCommitIsCorrect_dst"
	check(Init(dstRepo), t)

	// Verify that the commits "mycommit1" and "mycommit2" do not exist in destination:
	CheckNoExists(fmt.Sprintf("%s/mycommit1", dstRepo), t)
	CheckNoExists(fmt.Sprintf("%s/mycommit2", dstRepo), t)

	// Run a Pull/Recv operation to fetch all commits:
	check(Pull(srcRepo, "", NewLocalReplica(dstRepo)), t)

	// Verify that the commit "mycommit1" does not exist and "mycommit2" does in the destination repo:
	t.Skipf("TODO(jd,rw): no files were synced")
	CheckNoExists(fmt.Sprintf("%s/mycommit1/myfile1", dstRepo), t)
	CheckFile(fmt.Sprintf("%s/mycommit2/myfile2", dstRepo), "bar", t)
}

// TestBranchesAreNotImplicitlyReplicated // this is a known property, but not desirable long term
func TestBranchesAreNotImplicitlyReplicated(t *testing.T) {
	// Create a source repo:
	srcRepo := "repo_TestBranchesAreNotImplicitlyReplicated_src"
	check(Init(srcRepo), t)

	// Create a commit in the source repo:
	check(Commit(srcRepo, "mycommit", "master"), t)

	// Create a branch in the source repo:
	check(Branch(srcRepo, "mycommit", "mybranch"), t)

	// Create a destination repo:
	dstRepo := "repo_TestBranchesAreNotImplicitlyReplicated_dst"
	check(Init(dstRepo), t)

	// Run a Pull/Recv operation to fetch all commits on master:
	check(Pull(srcRepo, "", NewLocalReplica(dstRepo)), t)

	// Verify that only the commits are replicated, not branches:
	commitFilename := fmt.Sprintf("%s/mycommit", dstRepo)
	exists, err := FileExists(commitFilename)
	check(err, t)
	if !exists {
		t.Fatalf("File %s should exist.", commitFilename)
	}
	CheckNoExists(fmt.Sprintf("%s/mybranch", dstRepo), t)
}

func TestS3Replica(t *testing.T) {
	t.Skip("This test is periodically failing to reach s3.")
	// Create a source repo:
	srcRepo := "repo_TestS3Replica_src"
	check(Init(srcRepo), t)

	// Create a file in the source repo:
	writeFile(fmt.Sprintf("%s/master/myfile1", srcRepo), "foo", t)
	if testing.Short() {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 1, t)
	} else {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 10, t)
	}

	// Create a commit in the source repo:
	check(Commit(srcRepo, "mycommit1", "master"), t)

	// Create another file in the source repo:
	writeFile(fmt.Sprintf("%s/master/myfile2", srcRepo), "bar", t)
	if testing.Short() {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 1, t)
	} else {
		writeLots(fmt.Sprintf("%s/master/big_file", srcRepo), 10, t)
	}

	// Create a another commit in the source repo:
	check(Commit(srcRepo, "mycommit2", "master"), t)

	// Create a destination repo:
	dstRepo := "repo_TestS3Replica_dst"
	check(Init(dstRepo), t)

	// Verify that the commits "mycommit1" and "mycommit2" do exist in source:
	CheckFile(fmt.Sprintf("%s/mycommit1/myfile1", srcRepo), "foo", t)
	CheckFile(fmt.Sprintf("%s/mycommit2/myfile2", srcRepo), "bar", t)

	// Verify that the commits "mycommit1" and "mycommit2" do not exist in destination:
	CheckNoExists(fmt.Sprintf("%s/mycommit1", dstRepo), t)
	CheckNoExists(fmt.Sprintf("%s/mycommit2", dstRepo), t)

	// Run a Pull to push all commits to s3
	s3Replica := NewS3Replica(path.Join("pachyderm-test", randSeq(20)))
	err := Pull(srcRepo, "", s3Replica)
	check(err, t)

	// Pull commits from s3 to a new local replica
	err = s3Replica.Pull("", NewLocalReplica(dstRepo))
	check(err, t)

	// Verify that files from both commits are present:
	CheckFile(fmt.Sprintf("%s/mycommit1/myfile1", dstRepo), "foo", t)
	CheckFile(fmt.Sprintf("%s/mycommit2/myfile2", dstRepo), "bar", t)
	CheckFile(fmt.Sprintf("%s/master/myfile1", dstRepo), "foo", t)
	CheckFile(fmt.Sprintf("%s/master/myfile2", dstRepo), "bar", t)
}

// Test for `Commits`: check that the sort order of CommitInfo objects is structured correctly.
// Start from:
//	// Print BTRFS hierarchy data for humans:
//	check(Log("repo", "0", func(r io.Reader) error {
//		_, err := io.Copy(os.Stdout, r)
//		return err
//	}), t)

// TestFindNew, which is basically like `git diff`. Corresponds to `find-new` in btrfs.
func TestFindNew(t *testing.T) {
	repoName := "repo_TestFindNew"
	check(Init(repoName), t)

	checkFindNew := func(want []string, repo, from, to string) {
		got, err := FindNew(repo, from, to)
		check(err, t)
		t.Logf("checkFindNew(%v, %v, %v) -> %v", repo, from, to, got)

		// handle nil and empty slice the same way:
		if len(want) == 0 && len(got) == 0 {
			return
		}

		if !reflect.DeepEqual(want, got) {
			t.Fatalf("wanted %v, got %v for FindNew(%v, %v, %v)", want, got, repo, from, to)
		}

	}

	// There are no new files upon repo creation:
	check(Commit(repoName, "mycommit0", "master"), t)
	checkFindNew([]string{}, repoName, "mycommit0", "mycommit0")

	// A new, uncommited file is returned in the list:
	writeFile(fmt.Sprintf("%s/master/myfile1", repoName), "foo", t)
	checkFindNew([]string{"myfile1"}, repoName, "mycommit0", "master")

	// When that file is commited, then it still shows up in the delta since transid0:
	check(Commit(repoName, "mycommit1", "master"), t)
	// TODO(rw, jd) Shouldn't this pass?
	checkFindNew([]string{"myfile1"}, repoName, "mycommit0", "mycommit1")

	// The file doesn't show up in the delta since the new transaction:
	checkFindNew([]string{}, repoName, "mycommit1", "mycommit1")

	// Sanity check: the old delta still gives the same result:
	checkFindNew([]string{"myfile1"}, repoName, "mycommit0", "master")
}

func TestFilenamesWithSpaces(t *testing.T) {
	repoName := "repo_TestFilenamesWithSpaces"
	check(Init(repoName), t)

	fn := fmt.Sprintf("%s/master/my file", repoName)
	writeFile(fn, "some content", t)
	CheckFile(fn, "some content", t)
}

func TestFilenamesWithSlashesFail(t *testing.T) {
	repoName := "repo_TestFilenamesWithSlashesFail"
	check(Init(repoName), t)

	fn := fmt.Sprintf("%s/master/my/file", repoName)
	_, err := Create(fn)
	if err == nil {
		t.Fatalf("expected filename with slash to fail")
	}
}

func TestTwoSources(t *testing.T) {
	src1 := "repo_TestTwoSources_src1"
	check(Init(src1), t)
	src2 := "repo_TestTwoSources_src2"
	check(Init(src2), t)
	dst := "repo_TestTwoSources_dst"
	check(Init(dst), t)

	// write a file to src1
	writeFile(fmt.Sprintf("%s/master/file1", src1), "file1", t)
	// commit it
	check(Commit(src1, "commit1", "master"), t)
	// push it to src2
	check(NewLocalReplica(src1).Pull("", NewLocalReplica(src2)), t)
	// push it to dst
	check(NewLocalReplica(src1).Pull("", NewLocalReplica(dst)), t)

	writeFile(fmt.Sprintf("%s/master/file2", src2), "file2", t)
	check(Commit(src2, "commit2", "master"), t)
	check(NewLocalReplica(src2).Pull("commit1", NewLocalReplica(dst)), t)

	CheckFile(fmt.Sprintf("%s/commit1/file1", dst), "file1", t)
	CheckFile(fmt.Sprintf("%s/commit2/file2", dst), "file2", t)
}

func TestWaitFile(t *testing.T) {
	src := "repo_TestWaitFile"
	check(Init(src), t)
	complete := make(chan struct{})
	go func() {
		check(WaitFile(src+"/file", nil), t)
		complete <- struct{}{}
	}()
	WriteFile(src+"/file", nil)
	select {
	case <-complete:
		// we passed the test
		return
	case <-time.After(time.Second * 10):
		t.Fatal("Timeout waiting for file.")
	}
}

func TestCancelWaitFile(t *testing.T) {
	src := "repo_TestCancelWaitFile"
	check(Init(src), t)
	complete := make(chan struct{})
	cancel := make(chan struct{})
	go func() {
		err := WaitFile(src+"/file", cancel)
		if err != ErrCancelled {
			t.Fatal("Got the wrong error. Expected ErrCancelled.")
		}
		complete <- struct{}{}
	}()
	cancel <- struct{}{}
	select {
	case <-complete:
		// we passed the test
		return
	case <-time.After(time.Second * 10):
		t.Fatal("Timeout waiting for file.")
	}
}

func TestWaitAnyFile(t *testing.T) {
	src := "repo_TestWaitAnyFile"
	check(Init(src), t)
	complete := make(chan struct{})
	go func() {
		file, err := WaitAnyFile(src+"/file1", src+"/file2")
		log.Print("WaitedOn: ", file)
		check(err, t)
		if file != src+"/file2" {
			t.Fatal("Got the wrong file.")
		}
		complete <- struct{}{}
	}()
	WriteFile(src+"/file2", nil)
	select {
	case <-complete:
		// we passed the test
		return
	case <-time.After(time.Second * 10):
		t.Fatal("Timeout waiting for file.")
	}
}

// Case: create, delete, edit files and check that the filenames correspond to the changes ones.

// go test coverage