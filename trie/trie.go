// Package trie implements Merkle Patricia Tries.
package trie

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// emptyState is the known hash of an empty state trie entry.
	emptyState = crypto.Keccak256Hash(nil)
)

// LeafCallback is a callback type invoked when a trie operation reaches a leaf
// node.
//
// The paths is a path tuple identifying a particular trie node either in a single
// trie (account) or a layered trie (account -> storage). Each path in the tuple
// is in the raw format(32 bytes).
//
// The hexpath is a composite hexary path identifying the trie node. All the key
// bytes are converted to the hexary nibbles and composited with the parent path
// if the trie node is in a layered trie.
//
// It's used by state sync and commit to allow handling external references
// between account and storage tries. And also it's used in the state healing
// for extracting the raw states(leaf nodes) with corresponding paths.
type LeafCallback func(paths [][]byte, hexpath []byte, leaf []byte, parent common.Hash) error

// Trie is a Merkle Patricia Trie.
// Trie是Merkle Patricia Trie。
// The zero value is an empty trie with no database.
// 零值是没有数据库的空trie。
// Use New to create a trie that sits on top of a database.
// 使用New创建位于数据库顶部的trie。
// Trie is not safe for concurrent use.
// 同时使用Trie是不安全的。
type Trie struct {
	// 用levelDB做KV存储
	db *Database

	//当前根节点
	root node

	//启动加载的时候的hash值，通过这个hash值可以在数据库里面恢复出整颗的trie树
	owner common.Hash

	// Keep track of the number leaves which have been inserted since the last
	// hashing operation. This number will not directly map to the number of
	// actually unhashed nodes
	// 跟踪自上次哈希操作以来插入的叶数。该数字不会直接映射到实际未剪切节点的数量
	unhashed int

	// tracer is the state diff tracer can be used to track newly added/deleted
	// trie node. It will be reset after each commit operation.
	//跟踪器是状态差异跟踪器，可用于跟踪新添加/删除的trie节点。它将在每次提交操作后重置。
	tracer *tracer
}

// newFlag returns the cache flag value for a newly created node.
func (t *Trie) newFlag() nodeFlag {
	return nodeFlag{dirty: true}
}

// Copy returns a copy of Trie.
func (t *Trie) Copy() *Trie {
	return &Trie{
		db:       t.db,
		root:     t.root,
		owner:    t.owner,
		unhashed: t.unhashed,
		tracer:   t.tracer.copy(),
	}
}

// New creates a trie with an existing root node from db and an assigned
// owner for storage proximity.
// New使用db中的现有根节点和为存储邻近性分配的所有者创建trie。

// If root is the zero hash or the sha3 hash of an empty string, the
// trie is initially empty and does not require a database. Otherwise,
// New will panic if db is nil and returns a MissingNodeError if root does
// not exist in the database. Accessing the trie loads nodes from db on demand.

// 如果root是空字符串的零散列或sha3散列，则trie最初为空，不需要数据库。
// 否则，如果db为nil，New将死机，如果数据库中不存在根，则返回MissingNodeError。访问trie会根据需要从db加载节点。
// 数据库存在的话，如果传入的root为真实的不为空，那从数据库根据root恢复trie  否则以ower创建一个新的trie
func New(owner common.Hash, root common.Hash, db *Database) (*Trie, error) {
	return newTrie(owner, root, db)
}

// NewEmpty is a shortcut to create empty tree. It's mostly used in tests.
func NewEmpty(db *Database) *Trie {
	tr, _ := newTrie(common.Hash{}, common.Hash{}, db)
	return tr
}

// newWithRootNode initializes the trie with the given root node.
// It's only used by range prover.
func newWithRootNode(root node) *Trie {
	return &Trie{
		root: root,
		//tracer: newTracer(),
		db: NewDatabase(rawdb.NewMemoryDatabase()),
	}
}

// newTrie is the internal function used to construct the trie with given parameters.
func newTrie(owner common.Hash, root common.Hash, db *Database) (*Trie, error) {

	if db == nil {
		panic("trie.New called without a database")
	}

	trie := &Trie{
		db:    db,
		owner: owner,
		//tracer: newTracer(),
	}

	// 如果hash不是空值并且不是emptyRoot，从数据库中加载一个已经存在的树
	if root != (common.Hash{}) && root != emptyRoot {
		// 这里的trie.resolveHash就是加载整课树的方法
		rootnode, err := trie.resolveHash(root[:], nil)
		if err != nil {
			return nil, err
		}
		// 设置trie的根节点为从数据库中恢复出来的根节点
		trie.root = rootnode
	}
	//否则直接返回新建一个树
	return trie, nil
}

// NodeIterator returns an iterator that returns nodes of the trie. Iteration starts at
// the key after the given start key.
func (t *Trie) NodeIterator(start []byte) NodeIterator {
	return newNodeIterator(t, start)
}

// Get returns the value for key stored in the trie.
// The value bytes must not be modified by the caller.
// et返回存储在trie中的键的值
// 调用者不得修改值字节

// 传入节点key  获得存在trie的value值
func (t *Trie) Get(key []byte) []byte {

	res, err := t.TryGet(key)
	if err != nil {
		log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
	}
	return res
}

// TryGet returns the value for key stored in the trie.
// The value bytes must not be modified by the caller.
// If a node was not found in the database, a MissingNodeError is returned.
// TryGet返回存储在trie中的键的值
// 调用者不得修改值字节
// 如果在数据库中未找到节点，则返回MissingNodeError

func (t *Trie) TryGet(key []byte) ([]byte, error) {
	value, newroot, didResolve, err := t.tryGet(t.root, keybytesToHex(key), 0)
	if err == nil && didResolve {
		t.root = newroot
	}
	return value, err
}

// 根据root遍历MPT节点
func (t *Trie) tryGet(origNode node, key []byte, pos int) (value []byte, newnode node, didResolve bool, err error) {
	switch n := (origNode).(type) {
	case nil:
		return nil, nil, false, nil
	case valueNode:
		return n, n, false, nil
	case *shortNode:
		// key不存在
		if len(key)-pos < len(n.Key) || !bytes.Equal(n.Key, key[pos:pos+len(n.Key)]) {
			// key not found in trie
			return nil, n, false, nil
		}
		// 递归遍历
		value, newnode, didResolve, err = t.tryGet(n.Val, key, pos+len(n.Key))

		if err == nil && didResolve {
			n = n.copy()
			n.Val = newnode
		}
		return value, n, didResolve, err

	case *fullNode:
		// 递归遍历
		value, newnode, didResolve, err = t.tryGet(n.Children[key[pos]], key, pos+1)
		if err == nil && didResolve {
			n = n.copy()
			n.Children[key[pos]] = newnode
		}
		return value, n, didResolve, err
	case hashNode:
		child, err := t.resolveHash(n, key[:pos])
		if err != nil {
			return nil, n, true, err
		}
		value, newnode, _, err := t.tryGet(child, key, pos)
		return value, newnode, true, err
	default:
		panic(fmt.Sprintf("%T: invalid node: %v", origNode, origNode))
	}
}

// TryGetNode attempts to retrieve a trie node by compact-encoded path. It is not
// possible to use keybyte-encoding as the path might contain odd nibbles.
func (t *Trie) TryGetNode(path []byte) ([]byte, int, error) {
	item, newroot, resolved, err := t.tryGetNode(t.root, compactToHex(path), 0)
	if err != nil {
		return nil, resolved, err
	}
	if resolved > 0 {
		t.root = newroot
	}
	if item == nil {
		return nil, resolved, nil
	}
	return item, resolved, err
}

func (t *Trie) tryGetNode(origNode node, path []byte, pos int) (item []byte, newnode node, resolved int, err error) {
	// If non-existent path requested, abort
	if origNode == nil {
		return nil, nil, 0, nil
	}
	// If we reached the requested path, return the current node
	if pos >= len(path) {
		// Although we most probably have the original node expanded, encoding
		// that into consensus form can be nasty (needs to cascade down) and
		// time consuming. Instead, just pull the hash up from disk directly.
		var hash hashNode
		if node, ok := origNode.(hashNode); ok {
			hash = node
		} else {
			hash, _ = origNode.cache()
		}
		if hash == nil {
			return nil, origNode, 0, errors.New("non-consensus node")
		}
		blob, err := t.db.Node(common.BytesToHash(hash))
		return blob, origNode, 1, err
	}
	// Path still needs to be traversed, descend into children
	switch n := (origNode).(type) {
	case valueNode:
		// Path prematurely ended, abort
		return nil, nil, 0, nil

	case *shortNode:
		if len(path)-pos < len(n.Key) || !bytes.Equal(n.Key, path[pos:pos+len(n.Key)]) {
			// Path branches off from short node
			return nil, n, 0, nil
		}
		item, newnode, resolved, err = t.tryGetNode(n.Val, path, pos+len(n.Key))
		if err == nil && resolved > 0 {
			n = n.copy()
			n.Val = newnode
		}
		return item, n, resolved, err

	case *fullNode:
		item, newnode, resolved, err = t.tryGetNode(n.Children[path[pos]], path, pos+1)
		if err == nil && resolved > 0 {
			n = n.copy()
			n.Children[path[pos]] = newnode
		}
		return item, n, resolved, err

	case hashNode:
		child, err := t.resolveHash(n, path[:pos])
		if err != nil {
			return nil, n, 1, err
		}
		item, newnode, resolved, err := t.tryGetNode(child, path, pos)
		return item, newnode, resolved + 1, err

	default:
		panic(fmt.Sprintf("%T: invalid node: %v", origNode, origNode))
	}
}

// Update associates key with value in the trie. Subsequent calls to
// Get will return value. If value has length zero, any existing value
// is deleted from the trie and calls to Get will return nil.
//
// The value bytes must not be modified by the caller while they are
// stored in the trie.
func (t *Trie) Update(key, value []byte) {
	if err := t.TryUpdate(key, value); err != nil {
		log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
	}
}

func (t *Trie) TryUpdateAccount(key []byte, acc *types.StateAccount) error {
	data, err := rlp.EncodeToBytes(acc)
	if err != nil {
		return fmt.Errorf("can't encode object at %x: %w", key[:], err)
	}
	return t.TryUpdate(key, data)
}

// TryUpdate associates key with value in the trie. Subsequent calls to
// Get will return value. If value has length zero, any existing value
// is deleted from the trie and calls to Get will return nil.
//
// The value bytes must not be modified by the caller while they are
// stored in the trie.
//
// If a node was not found in the database, a MissingNodeError is returned.

//TryUpdate将键与trie中的值相关联。对Get的后续调用将返回值。如果值的长度为零，则从trie中删除任何现有值，并且对Get的调用将返回nil。
//
//值字节存储在trie中时，调用者不得修改它们。
//
//如果在数据库中未找到节点，则返回MissingNodeError。

func (t *Trie) TryUpdate(key, value []byte) error {
	// 记录操作次数
	t.unhashed++
	k := keybytesToHex(key)
	fmt.Println("数据更新到trie的key对应的hex编码: ", k)
	// value的值长度不为0的话则为插入,为0的话为删除
	if len(value) != 0 {
		fmt.Println("空trie添加数据前的根节点: ", t.root)
		fmt.Println("即将插入的数据的key的hex: ", k)
		fmt.Println("即将插入的数据的value: ", valueNode(value))
		_, n, err := t.insert(t.root, nil, k, valueNode(value))
		fmt.Println("数据插入tire以后返回的节点", n)
		if err != nil {
			return err
		}
		t.root = n
		fmt.Println("插入完成以后的根节点: ", t.root)
	} else {
		_, n, err := t.delete(t.root, nil, k)
		if err != nil {
			return err
		}
		t.root = n
	}
	return nil
}

/*
	insert	MPT树节点的插入操作
	node	要插入哪个节点
	prefix	当前已处理完的key(节点共有的前缀)
	key		当前未处理的key(完整key = prefix + key)
	value	当前插入的值

	bool	返回函数是否改变了MPT树
	node	执行插入后的MPT树根节点
*/
//  是在向节点n插入节点value
func (t *Trie) insert(n node, prefix, key []byte, value node) (bool, node, error) {
	// key的长度为0
	if len(key) == 0 {
		// 如果是数据节点的话
		if v, ok := n.(valueNode); ok {

			return !bytes.Equal(v, value.(valueNode)), value, nil
		}
		// 不是数据节点的话
		return true, value, nil
	}

	// 判断是那种类型的节点
	switch n := n.(type) {
	case *shortNode:
		// 如果是叶子节点，首先计算共有前缀长度
		matchlen := prefixLen(key, n.Key)
		// If the whole key matches, keep this short node as is
		// and only update the value.
		// 如果相同key的长度正好等于及诶单的key长度  说明节点已经存在  只更新节点的value即可
		if matchlen == len(n.Key) {
			// 递归
			dirty, nn, err := t.insert(n.Val, append(prefix, key[:matchlen]...), key[matchlen:], value)
			if !dirty || err != nil {
				return false, n, err
			}
			return true, &shortNode{n.Key, nn, t.newFlag()}, nil
		}
		// Otherwise branch out at the index where they differ.
		// 构造形成一个分支节点(fullNode)
		branch := &fullNode{flags: t.newFlag()}
		var err error

		// 将原来的节点拆作新的后缀shortNode插入
		_, branch.Children[n.Key[matchlen]], err = t.insert(nil, append(prefix, n.Key[:matchlen+1]...), n.Key[matchlen+1:], n.Val)
		if err != nil {
			return false, nil, err
		}
		//将新节点作为shortNode插入
		_, branch.Children[key[matchlen]], err = t.insert(nil, append(prefix, key[:matchlen+1]...), key[matchlen+1:], value)
		if err != nil {
			return false, nil, err
		}
		// Replace this shortNode with the branch if it occurs at index 0.
		// 如果没有共有的前缀，则新建的分支节点为根节点
		if matchlen == 0 {
			return true, branch, nil
		}
		// New branch node is created as a child of the original short node.
		// Track the newly inserted node in the tracer. The node identifier
		// passed is the path from the root node.
		t.tracer.onInsert(append(prefix, key[:matchlen]...))

		// Replace it with a short node leading up to the branch.
		// 如果有共有的前缀，则拆分原节点产生前缀叶子节点为根节点   把前缀弄成根节点  然后指向branch
		return true, &shortNode{key[:matchlen], branch, t.newFlag()}, nil

	case *fullNode:
		// 若果是分支节点，则直接将新数据插入作为子节点
		dirty, nn, err := t.insert(n.Children[key[0]], append(prefix, key[0]), key[1:], value)
		if !dirty || err != nil {
			return false, n, err
		}
		n = n.copy()
		n.flags = t.newFlag()
		n.Children[key[0]] = nn
		return true, n, nil

	case nil:

		// New short node is created and track it in the tracer. The node identifier
		// passed is the path from the root node. Note the valueNode won't be tracked
		// since it's always embedded in its parent.

		// 创建新的短节点并在跟踪器中跟踪它。传递的节点标识符是来自根节点的路径。注意，不会跟踪valueNode，因为它始终嵌入在其父节点中
		// 像一个空节点插入节点，就是返回一个shortnode
		t.tracer.onInsert(prefix)
		return true, &shortNode{key, value, t.newFlag()}, nil

	case hashNode:
		// We've hit a part of the trie that isn't loaded yet. Load
		// the node and insert into it. This leaves all child nodes on
		// the path to the value in the trie.
		// 哈希节点 表示当前节点还未加载到内存中，首先需要调用resolveHash从数据库中加载节点
		rn, err := t.resolveHash(n, prefix)
		if err != nil {
			return false, nil, err
		}
		// 然后在该节点后插入新节点
		dirty, nn, err := t.insert(rn, prefix, key, value)
		if !dirty || err != nil {
			return false, rn, err
		}
		return true, nn, nil

	default:
		panic(fmt.Sprintf("%T: invalid node: %v", n, n))
	}
}

// Delete removes any existing value for key from the trie.
func (t *Trie) Delete(key []byte) {
	if err := t.TryDelete(key); err != nil {
		log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
	}
}

// TryDelete removes any existing value for key from the trie.
// If a node was not found in the database, a MissingNodeError is returned.
func (t *Trie) TryDelete(key []byte) error {
	t.unhashed++
	k := keybytesToHex(key)
	_, n, err := t.delete(t.root, nil, k)
	if err != nil {
		return err
	}
	t.root = n
	return nil
}

// delete returns the new root of the trie with key deleted.
// It reduces the trie to minimal form by simplifying
// nodes on the way up after deleting recursively.
func (t *Trie) delete(n node, prefix, key []byte) (bool, node, error) {
	switch n := n.(type) {
	case *shortNode:
		matchlen := prefixLen(key, n.Key)
		if matchlen < len(n.Key) {
			return false, n, nil // don't replace n on mismatch
		}
		if matchlen == len(key) {
			// The matched short node is deleted entirely and track
			// it in the deletion set. The same the valueNode doesn't
			// need to be tracked at all since it's always embedded.
			t.tracer.onDelete(prefix)

			return true, nil, nil // remove n entirely for whole matches
		}
		// The key is longer than n.Key. Remove the remaining suffix
		// from the subtrie. Child can never be nil here since the
		// subtrie must contain at least two other values with keys
		// longer than n.Key.
		dirty, child, err := t.delete(n.Val, append(prefix, key[:len(n.Key)]...), key[len(n.Key):])
		if !dirty || err != nil {
			return false, n, err
		}
		switch child := child.(type) {
		case *shortNode:
			// The child shortNode is merged into its parent, track
			// is deleted as well.
			t.tracer.onDelete(append(prefix, n.Key...))

			// Deleting from the subtrie reduced it to another
			// short node. Merge the nodes to avoid creating a
			// shortNode{..., shortNode{...}}. Use concat (which
			// always creates a new slice) instead of append to
			// avoid modifying n.Key since it might be shared with
			// other nodes.
			return true, &shortNode{concat(n.Key, child.Key...), child.Val, t.newFlag()}, nil
		default:
			return true, &shortNode{n.Key, child, t.newFlag()}, nil
		}

	case *fullNode:
		dirty, nn, err := t.delete(n.Children[key[0]], append(prefix, key[0]), key[1:])
		if !dirty || err != nil {
			return false, n, err
		}
		n = n.copy()
		n.flags = t.newFlag()
		n.Children[key[0]] = nn

		// Because n is a full node, it must've contained at least two children
		// before the delete operation. If the new child value is non-nil, n still
		// has at least two children after the deletion, and cannot be reduced to
		// a short node.
		if nn != nil {
			return true, n, nil
		}
		// Reduction:
		// Check how many non-nil entries are left after deleting and
		// reduce the full node to a short node if only one entry is
		// left. Since n must've contained at least two children
		// before deletion (otherwise it would not be a full node) n
		// can never be reduced to nil.
		//
		// When the loop is done, pos contains the index of the single
		// value that is left in n or -2 if n contains at least two
		// values.
		pos := -1
		for i, cld := range &n.Children {
			if cld != nil {
				if pos == -1 {
					pos = i
				} else {
					pos = -2
					break
				}
			}
		}
		if pos >= 0 {
			if pos != 16 {
				// If the remaining entry is a short node, it replaces
				// n and its key gets the missing nibble tacked to the
				// front. This avoids creating an invalid
				// shortNode{..., shortNode{...}}.  Since the entry
				// might not be loaded yet, resolve it just for this
				// check.
				cnode, err := t.resolve(n.Children[pos], prefix)
				if err != nil {
					return false, nil, err
				}
				if cnode, ok := cnode.(*shortNode); ok {
					// Replace the entire full node with the short node.
					// Mark the original short node as deleted since the
					// value is embedded into the parent now.
					t.tracer.onDelete(append(prefix, byte(pos)))

					k := append([]byte{byte(pos)}, cnode.Key...)
					return true, &shortNode{k, cnode.Val, t.newFlag()}, nil
				}
			}
			// Otherwise, n is replaced by a one-nibble short node
			// containing the child.
			return true, &shortNode{[]byte{byte(pos)}, n.Children[pos], t.newFlag()}, nil
		}
		// n still contains at least two values and cannot be reduced.
		return true, n, nil

	case valueNode:
		return true, nil, nil

	case nil:
		return false, nil, nil

	case hashNode:
		// We've hit a part of the trie that isn't loaded yet. Load
		// the node and delete from it. This leaves all child nodes on
		// the path to the value in the trie.
		rn, err := t.resolveHash(n, prefix)
		if err != nil {
			return false, nil, err
		}
		dirty, nn, err := t.delete(rn, prefix, key)
		if !dirty || err != nil {
			return false, rn, err
		}
		return true, nn, nil

	default:
		panic(fmt.Sprintf("%T: invalid node: %v (%v)", n, n, key))
	}
}

func concat(s1 []byte, s2 ...byte) []byte {
	r := make([]byte, len(s1)+len(s2))
	copy(r, s1)
	copy(r[len(s1):], s2)
	return r
}

func (t *Trie) resolve(n node, prefix []byte) (node, error) {
	if n, ok := n.(hashNode); ok {
		return t.resolveHash(n, prefix)
	}
	return n, nil
}

func (t *Trie) resolveHash(n hashNode, prefix []byte) (node, error) {
	hash := common.BytesToHash(n)
	//通过hash从db中取出node的RLP编码内容
	if node := t.db.node(hash); node != nil {
		return node, nil
	}
	return nil, &MissingNodeError{Owner: t.owner, NodeHash: hash, Path: prefix}
}

func (t *Trie) resolveBlob(n hashNode, prefix []byte) ([]byte, error) {
	hash := common.BytesToHash(n)
	blob, _ := t.db.Node(hash)
	if len(blob) != 0 {
		return blob, nil
	}
	return nil, &MissingNodeError{Owner: t.owner, NodeHash: hash, Path: prefix}
}

// Hash returns the root hash of the trie. It does not write to the
// database and can be used even if the trie doesn't have one.
// 哈希返回trie的根哈希。它不会写入数据库，即使trie没有数据库，也可以使用它。
func (t *Trie) Hash() common.Hash {
	hash, cached, _ := t.hashRoot()
	t.root = cached
	return common.BytesToHash(hash.(hashNode))
}

// Commit writes all nodes to the trie's memory database, tracking the internal
// and external (for account tries) references.
// 提交将所有节点写入trie的内存数据库，跟踪内部和外部（用于帐户尝试）引用
// 序列化MPT树，并将所有节点数据存储到数据库中
func (t *Trie) Commit(onleaf LeafCallback) (common.Hash, int, error) {
	// 没数据库
	if t.db == nil {
		panic("commit called on trie with nil database")
	}
	defer t.tracer.reset()

	// 没有根节点  就是空的trie
	if t.root == nil {
		return emptyRoot, 0, nil
	}
	// Derive the hash for all dirty nodes first. We hold the assumption
	// in the following procedure that all nodes are hashed.
	// 首先导出所有脏节点的哈希，我们在下面的过程中假设所有节点都是散列的
	// 这个是在获取根Hash
	rootHash := t.Hash()
	// 先去看下怎么获取到这个根hash

	h := newCommitter()
	defer returnCommitterToPool(h)

	// Do a quick check if we really need to commit, before we spin
	// up goroutines. This can happen e.g. if we load a trie for reading storage
	// values, but don't write to it.
	if hashedNode, dirty := t.root.cache(); !dirty {
		// Replace the root node with the origin hash in order to
		// ensure all resolved nodes are dropped after the commit.
		t.root = hashedNode
		return rootHash, 0, nil
	}
	var wg sync.WaitGroup
	if onleaf != nil {
		h.onleaf = onleaf
		h.leafCh = make(chan *leaf, leafChanSize)
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.commitLoop(t.db)
		}()
	}
	newRoot, committed, err := h.Commit(t.root, t.db)
	if onleaf != nil {
		// The leafch is created in newCommitter if there was an onleaf callback
		// provided. The commitLoop only _reads_ from it, and the commit
		// operation was the sole writer. Therefore, it's safe to close this
		// channel here.
		close(h.leafCh)
		wg.Wait()
	}
	if err != nil {
		return common.Hash{}, 0, err
	}
	t.root = newRoot
	return rootHash, committed, nil
}

// hashRoot calculates the root hash of the given trie
// hashRoot计算给定trie的根哈希
// 折叠MPT节点的实现
func (t *Trie) hashRoot() (node, node, error) {
	// 空的根节点 返回空根节点的对应的hash()
	if t.root == nil {
		return hashNode(emptyRoot.Bytes()), nil, nil
	}
	// If the number of changes is below 100, we let one thread handle it
	// 如果更改的数量低于100，我们让一个线程处理它
	h := newHasher(t.unhashed >= 100)
	defer returnHasherToPool(h)

	hashed, cached := h.hash(t.root, true)
	t.unhashed = 0
	return hashed, cached, nil
}

// Reset drops the referenced root node and cleans all internal state.
func (t *Trie) Reset() {
	t.root = nil
	t.owner = common.Hash{}
	t.unhashed = 0
	t.tracer.reset()
}

// Owner returns the associated trie owner.
func (t *Trie) Owner() common.Hash {
	return t.owner
}
