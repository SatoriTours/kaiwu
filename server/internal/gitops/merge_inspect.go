package gitops

import (
	"context"
	"errors"
	"strings"
)

var errNotCommit = errors.New("提交标识无效或对象不可用")

// CommitParents returns the parent commits of one commit, in order. It only
// reads objects that already exist locally.
func CommitParents(ctx context.Context, dir, sha string) ([]string, error) {
	if len(sha) != 40 || !isHex(sha) {
		return nil, errNotCommit
	}
	out, e := output(ctx, dir, "rev-list", "--parents", "-n", "1", sha)
	if e != nil {
		return nil, e
	}
	fields := strings.Fields(out)
	if len(fields) == 0 || fields[0] != sha {
		return nil, errNotCommit
	}
	return fields[1:], nil
}

// TreeOf returns the tree object of one locally present commit.
func TreeOf(ctx context.Context, dir, sha string) (string, error) {
	if len(sha) != 40 || !isHex(sha) {
		return "", errNotCommit
	}
	out, e := output(ctx, dir, "rev-parse", "--verify", sha+"^{tree}")
	if e != nil {
		return "", errNotCommit
	}
	tree := strings.TrimSpace(out)
	if len(tree) != 40 || !isHex(tree) {
		return "", errNotCommit
	}
	return tree, nil
}

// ExpectedMergeTree computes the tree an ordinary merge of exactly these two
// commits produces. It never moves a ref; the object database may gain the tree.
func ExpectedMergeTree(ctx context.Context, dir, a, b string) (string, error) {
	out, e := output(ctx, dir, "merge-tree", "--write-tree", a, b)
	if e != nil {
		return "", e
	}
	tree := strings.TrimSpace(out)
	if len(tree) != 40 || !isHex(tree) {
		return "", errNotCommit
	}
	return tree, nil
}
