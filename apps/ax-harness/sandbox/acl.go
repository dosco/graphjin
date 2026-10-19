package sandbox

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"syscall"
)

// POSIX ACL extended-attribute format (linux/posix_acl_xattr.h).
const (
	aclVersion  = 2
	aclUserObj  = 0x01
	aclGroupObj = 0x04
	aclGroup    = 0x08
	aclMask     = 0x10
	aclOther    = 0x20
	aclNoID     = 0xFFFFFFFF
	aclRWX      = 7
)

type aclEntry struct {
	tag  uint16
	perm uint16
	id   uint32
}

// groupACL grants rwx to the owner, the owning group and each of gids, and
// nothing to others.
func groupACL(gids []int) []byte {
	entries := []aclEntry{
		{aclUserObj, aclRWX, aclNoID},
		{aclGroupObj, aclRWX, aclNoID},
		{aclMask, aclRWX, aclNoID},
		{aclOther, 0, aclNoID},
	}
	for _, gid := range gids {
		entries = append(entries, aclEntry{aclGroup, aclRWX, uint32(gid)})
	}
	// The kernel requires entries ordered by tag, then id.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].tag != entries[j].tag {
			return entries[i].tag < entries[j].tag
		}
		return entries[i].id < entries[j].id
	})
	out := make([]byte, 4, 4+8*len(entries))
	binary.LittleEndian.PutUint32(out, aclVersion)
	for _, e := range entries {
		var b [8]byte
		binary.LittleEndian.PutUint16(b[0:], e.tag)
		binary.LittleEndian.PutUint16(b[2:], e.perm)
		binary.LittleEndian.PutUint32(b[4:], e.id)
		out = append(out, b[:]...)
	}
	return out
}

// errNoACL reports a runs folder whose filesystem has no POSIX ACLs.
var errNoACL = errors.New("the runs folder does not support POSIX ACLs, so runs cannot be kept apart")

// grantTree gives gids rwx on every file and folder under root, and makes
// the same grant the default for anything created in its folders later.
// Symbolic links are left alone.
func grantTree(root string, gids []int) error {
	acl := groupACL(gids)
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if err := syscall.Setxattr(path, "system.posix_acl_access", acl, 0); err != nil {
			if errors.Is(err, syscall.EOPNOTSUPP) {
				return errNoACL
			}
			return err
		}
		if entry.IsDir() {
			if err := syscall.Setxattr(path, "system.posix_acl_default", acl, 0); err != nil {
				if errors.Is(err, syscall.EOPNOTSUPP) {
					return errNoACL
				}
				return err
			}
		}
		return nil
	})
}
