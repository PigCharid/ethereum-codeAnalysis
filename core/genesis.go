package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

//go:generate go run github.com/fjl/gencodec -type Genesis -field-override genesisSpecMarshaling -out gen_genesis.go
//go:generate go run github.com/fjl/gencodec -type GenesisAccount -field-override genesisAccountMarshaling -out gen_genesis_account.go

// genesis没有链配置
var errGenesisNoConfig = errors.New("genesis has no chain configuration")

//Genesis指定头字段、Genesis块的状态。它还通过链配置定义了硬叉切换块。
type Genesis struct {
	// 配置文件 用于指定链的ID
	Config *params.ChainConfig `json:"config"`
	// 随机数
	Nonce uint64 `json:"nonce"`
	// 时间戳
	Timestamp uint64 `json:"timestamp"`
	// 区块的额外信息
	ExtraData []byte `json:"extraData"`
	// Gas消耗限制
	GasLimit uint64 `json:"gasLimit"   gencodec:"required"`
	// 区块难度值
	Difficulty *big.Int `json:"difficulty" gencodec:"required"`
	// 由上个区块的一部分生成的Hash，和Nonce组合用于找到满足POW算法的条件
	Mixhash common.Hash `json:"mixHash"`
	// 矿工地址
	Coinbase common.Address `json:"coinbase"`
	// 创世区块初始状态
	Alloc GenesisAlloc `json:"alloc"      gencodec:"required"`

	//这些字段用于共识测试，请不要在实际的genesis区块中使用它们。
	Number  uint64 `json:"number"`
	GasUsed uint64 `json:"gasUsed"`
	// 父区块哈希
	ParentHash common.Hash `json:"parentHash"`
	BaseFee    *big.Int    `json:"baseFeePerGas"`
}

// GenesisAlloc specifies the initial state that is part of the genesis block.
type GenesisAlloc map[common.Address]GenesisAccount

func (ga *GenesisAlloc) UnmarshalJSON(data []byte) error {
	m := make(map[common.UnprefixedAddress]GenesisAccount)
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*ga = make(GenesisAlloc)
	for addr, a := range m {
		(*ga)[common.Address(addr)] = a
	}
	return nil
}

// flush adds allocated genesis accounts into a fresh new statedb and
// commit the state changes into the given database handler.
func (ga *GenesisAlloc) flush(db ethdb.Database) (common.Hash, error) {
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	if err != nil {
		return common.Hash{}, err
	}
	for addr, account := range *ga {
		statedb.AddBalance(addr, account.Balance)
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}
	root, err := statedb.Commit(false)
	if err != nil {
		return common.Hash{}, err
	}
	err = statedb.Database().TrieDB().Commit(root, true, nil)
	if err != nil {
		return common.Hash{}, err
	}
	return root, nil
}

// write writes the json marshaled genesis state into database
// with the given block hash as the unique identifier.
func (ga *GenesisAlloc) write(db ethdb.KeyValueWriter, hash common.Hash) error {
	blob, err := json.Marshal(ga)
	if err != nil {
		return err
	}
	rawdb.WriteGenesisState(db, hash, blob)
	return nil
}

// CommitGenesisState loads the stored genesis state with the given block
// hash and commits them into the given database handler.
func CommitGenesisState(db ethdb.Database, hash common.Hash) error {
	var alloc GenesisAlloc
	blob := rawdb.ReadGenesisState(db, hash)
	if len(blob) != 0 {
		if err := alloc.UnmarshalJSON(blob); err != nil {
			return err
		}
	} else {
		// Genesis allocation is missing and there are several possibilities:
		// the node is legacy which doesn't persist the genesis allocation or
		// the persisted allocation is just lost.
		// - supported networks(mainnet, testnets), recover with defined allocations
		// - private network, can't recover
		var genesis *Genesis
		switch hash {
		case params.MainnetGenesisHash:
			genesis = DefaultGenesisBlock()
		case params.RopstenGenesisHash:
			genesis = DefaultRopstenGenesisBlock()
		case params.RinkebyGenesisHash:
			genesis = DefaultRinkebyGenesisBlock()
		case params.GoerliGenesisHash:
			genesis = DefaultGoerliGenesisBlock()
		case params.SepoliaGenesisHash:
			genesis = DefaultSepoliaGenesisBlock()
		}
		if genesis != nil {
			alloc = genesis.Alloc
		} else {
			return errors.New("not found")
		}
	}
	_, err := alloc.flush(db)
	return err
}

// GenesisAccount is an account in the state of the genesis block.
type GenesisAccount struct {
	Code       []byte                      `json:"code,omitempty"`
	Storage    map[common.Hash]common.Hash `json:"storage,omitempty"`
	Balance    *big.Int                    `json:"balance" gencodec:"required"`
	Nonce      uint64                      `json:"nonce,omitempty"`
	PrivateKey []byte                      `json:"secretKey,omitempty"` // for tests
}

// field type overrides for gencodec
type genesisSpecMarshaling struct {
	Nonce      math.HexOrDecimal64
	Timestamp  math.HexOrDecimal64
	ExtraData  hexutil.Bytes
	GasLimit   math.HexOrDecimal64
	GasUsed    math.HexOrDecimal64
	Number     math.HexOrDecimal64
	Difficulty *math.HexOrDecimal256
	BaseFee    *math.HexOrDecimal256
	Alloc      map[common.UnprefixedAddress]GenesisAccount
}

type genesisAccountMarshaling struct {
	Code       hexutil.Bytes
	Balance    *math.HexOrDecimal256
	Nonce      math.HexOrDecimal64
	Storage    map[storageJSON]storageJSON
	PrivateKey hexutil.Bytes
}

// storageJSON represents a 256 bit byte array, but allows less than 256 bits when
// unmarshaling from hex.
type storageJSON common.Hash

func (h *storageJSON) UnmarshalText(text []byte) error {
	text = bytes.TrimPrefix(text, []byte("0x"))
	if len(text) > 64 {
		return fmt.Errorf("too many hex characters in storage key/value %q", text)
	}
	offset := len(h) - len(text)/2 // pad on the left
	if _, err := hex.Decode(h[offset:], text); err != nil {
		fmt.Println(err)
		return fmt.Errorf("invalid hex storage key/value %q", text)
	}
	return nil
}

func (h storageJSON) MarshalText() ([]byte, error) {
	return hexutil.Bytes(h[:]).MarshalText()
}

// GenesisMismatchError is raised when trying to overwrite an existing
// genesis block with an incompatible one.
type GenesisMismatchError struct {
	Stored, New common.Hash
}

func (e *GenesisMismatchError) Error() string {
	return fmt.Sprintf("database contains incompatible genesis (have %x, new %x)", e.Stored, e.New)
}

// SetupGenesisBlock writes or updates the genesis block in db.
// SetupGenesisBlock在db中写入或更新genesis块。

// The block that will be used is:
// 将使用的块是
//
//                          genesis == nil       genesis != nil
//                       +------------------------------------------
//     db has no genesis |  main-net default  |  genesis
//     db has genesis    |  from DB           |  genesis (if compatible)
//
// The stored chain configuration will be updated if it is compatible (i.e. does not
// specify a fork block below the local head block). In case of a conflict, the
// error is a *params.ConfigCompatError and the new, unwritten config is returned.
// 如果存储链配置兼容（即未在本地头块下方指定叉块），则将更新存储链配置。如果发生冲突，则错误为*参数。ConfigCompatError并返回新的未写入配置。
// The returned chain configuration is never nil.
// 返回的链配置从不为空
func SetupGenesisBlock(db ethdb.Database, genesis *Genesis) (*params.ChainConfig, common.Hash, error) {
	return SetupGenesisBlockWithOverride(db, genesis, nil, nil)
}

func SetupGenesisBlockWithOverride(db ethdb.Database, genesis *Genesis, overrideGrayGlacier, overrideTerminalTotalDifficulty *big.Int) (*params.ChainConfig, common.Hash, error) {
	// 创世块为空或者创世快的配置信息为空
	if genesis != nil && genesis.Config == nil {
		return params.AllEthashProtocolChanges, common.Hash{}, errGenesisNoConfig
	}

	// 定义一个函数类型
	applyOverrides := func(config *params.ChainConfig) {
		if config != nil {

			if overrideTerminalTotalDifficulty != nil {
				config.TerminalTotalDifficulty = overrideTerminalTotalDifficulty
			}
			if overrideGrayGlacier != nil {
				config.GrayGlacierBlock = overrideGrayGlacier
			}
		}
	}

	// Just commit the new block if there is no stored genesis block.
	// 如果没有存储的genesis块，只需提交新块
	// 从数据库获取创世快的Hash
	stored := rawdb.ReadCanonicalHash(db, 0)

	// 数据库创世区块为空
	if (stored == common.Hash{}) {
		// 传入的创世块也为空
		if genesis == nil {
			// 写入默认主网创世快
			log.Info("Writing default main-net genesis block")
			// 创世快赋值
			genesis = DefaultGenesisBlock()
		} else {
			// 使用自定义的genesis配置
			log.Info("Writing custom genesis block")
		}
		// 创世区块写入数据库
		block, err := genesis.Commit(db)
		if err != nil {
			return genesis.Config, common.Hash{}, err
		}
		// 两个字段从新载入  应该是和挖矿难度相关
		applyOverrides(genesis.Config)
		// 创建成功 返回
		return genesis.Config, block.Hash(), nil
	}
	// We have the genesis block in database(perhaps in ancient database)
	// but the corresponding state is missing.
	// 我们在数据库中有genesis区块（可能在古代数据库中）
	// 但缺少相应的状态
	// 获取创世区块的状态信息
	header := rawdb.ReadHeader(db, stored, 0)

	if _, err := state.New(header.Root, state.NewDatabaseWithConfig(db, nil), nil); err != nil {
		if genesis == nil {
			genesis = DefaultGenesisBlock()
		}
		// 确保存储的genesis与给定的genesis匹配
		hash := genesis.ToBlock(nil).Hash()
		// 给定的genesis和数据库获取的对不上
		if hash != stored {
			return genesis.Config, hash, &GenesisMismatchError{stored, hash}
		}
		// 存数据库
		block, err := genesis.Commit(db)
		if err != nil {
			return genesis.Config, hash, err
		}
		applyOverrides(genesis.Config)
		return genesis.Config, block.Hash(), nil
	}

	// Check whether the genesis block is already written.
	// 检查创世快块是否已写入。
	if genesis != nil {
		hash := genesis.ToBlock(nil).Hash()
		if hash != stored {
			return genesis.Config, hash, &GenesisMismatchError{stored, hash}
		}
	}
	// 获取当前区块的相关信息
	newcfg := genesis.configOrDefault(stored)
	// 数据重载
	applyOverrides(newcfg)

	//CheckConfigForkOrder检查我们没有“跳过”任何fork，geth不足以保证fork可以以与官方网络不同的顺序实现
	if err := newcfg.CheckConfigForkOrder(); err != nil {
		return newcfg, common.Hash{}, err
	}
	//ReadChainConfig根据给定的genesis哈希检索共识设置
	storedcfg := rawdb.ReadChainConfig(db, stored)
	if storedcfg == nil {
		// 创世快信息没有写入
		log.Warn("Found genesis block without chain config")
		//WriteChainConfig将链配置设置写入数据库。
		rawdb.WriteChainConfig(db, stored, newcfg)
		return newcfg, stored, nil
	}
	// Special case: if a private network is being used (no genesis and also no
	// mainnet hash in the database), we must not apply the `configOrDefault`
	// chain config as that would be AllProtocolChanges (applying any new fork
	// on top of an existing private network genesis block). In that case, only
	// apply the overrides.
	//特例：如果正在使用私有网络（数据库中没有genesis，也没有mainnet哈希），我们不能应用'configOrDefault'链配置，
	//因为这将是AllProtocolChanges（在现有私有网络genesis块上应用任何新分支）。在这种情况下，仅应用覆盖。
	if genesis == nil && stored != params.MainnetGenesisHash {
		newcfg = storedcfg
		applyOverrides(newcfg)
	}
	// Check config compatibility and write the config. Compatibility errors
	// are returned to the caller unless we're already at block zero.
	// 检查配置兼容性并写入配置。兼容性错误将返回给调用者，除非我们已经处于块零。
	height := rawdb.ReadHeaderNumber(db, rawdb.ReadHeadHeaderHash(db))
	if height == nil {
		return newcfg, stored, fmt.Errorf("missing block number for head header hash")
	}
	compatErr := storedcfg.CheckCompatible(newcfg, *height)
	// 如果compatErr为空并且非高度为0的区块，那么久不能更改genesis配置了
	if compatErr != nil && *height != 0 && compatErr.RewindTo != 0 {
		return newcfg, stored, compatErr
	}
	rawdb.WriteChainConfig(db, stored, newcfg)
	return newcfg, stored, nil
}

func (g *Genesis) configOrDefault(ghash common.Hash) *params.ChainConfig {
	switch {
	case g != nil:
		return g.Config
	case ghash == params.MainnetGenesisHash:
		return params.MainnetChainConfig
	case ghash == params.RopstenGenesisHash:
		return params.RopstenChainConfig
	case ghash == params.SepoliaGenesisHash:
		return params.SepoliaChainConfig
	case ghash == params.RinkebyGenesisHash:
		return params.RinkebyChainConfig
	case ghash == params.GoerliGenesisHash:
		return params.GoerliChainConfig
	case ghash == params.KilnGenesisHash:
		return DefaultKilnGenesisBlock().Config
	default:
		return params.AllEthashProtocolChanges
	}
}

// ToBlock creates the genesis block and writes state of a genesis specification
// to the given database (or discards it if nil).
// ToBlock创建genesis块并将genesis规范的状态写入给定数据库
func (g *Genesis) ToBlock(db ethdb.Database) *types.Block {
	// 如果数据库为空 则创建
	if db == nil {
		db = rawdb.NewMemoryDatabase()
	}
	// flush将分配的genesis帐户添加到新的statedb中，并将状态更改提交到给定的数据库处理程序中。
	root, err := g.Alloc.flush(db)
	if err != nil {
		panic(err)
	}
	// 填充head
	head := &types.Header{
		Number:     new(big.Int).SetUint64(g.Number),
		Nonce:      types.EncodeNonce(g.Nonce),
		Time:       g.Timestamp,
		ParentHash: g.ParentHash,
		Extra:      g.ExtraData,
		GasLimit:   g.GasLimit,
		GasUsed:    g.GasUsed,
		BaseFee:    g.BaseFee,
		Difficulty: g.Difficulty,
		MixDigest:  g.Mixhash,
		Coinbase:   g.Coinbase,
		Root:       root,
	}
	// 几个字段重新赋值
	if g.GasLimit == 0 {
		head.GasLimit = params.GenesisGasLimit
	}
	if g.Difficulty == nil && g.Mixhash == (common.Hash{}) {
		head.Difficulty = params.GenesisDifficulty
	}
	if g.Config != nil && g.Config.IsLondon(common.Big0) {
		if g.BaseFee != nil {
			head.BaseFee = g.BaseFee
		} else {
			head.BaseFee = new(big.Int).SetUint64(params.InitialBaseFee)
		}
	}
	//组成block返回
	return types.NewBlock(head, nil, nil, nil, trie.NewStackTrie(nil))
}

// Commit writes the block and state of a genesis specification to the database.
// The block is committed as the canonical head block.
// 提交将genesis规范的块和状态写入数据库。
// 该块被提交为规范头块。
func (g *Genesis) Commit(db ethdb.Database) (*types.Block, error) {
	// 取到genesis对应的block
	block := g.ToBlock(db)
	if block.Number().Sign() != 0 {
		// 无法提交 number > 0 的创世区块
		return nil, errors.New("can't commit genesis block with number > 0")
	}

	config := g.Config
	if config == nil {
		config = params.AllEthashProtocolChanges
	}
	if err := config.CheckConfigForkOrder(); err != nil {
		return nil, err
	}
	if config.Clique != nil && len(block.Extra()) < 32+crypto.SignatureLength {
		return nil, errors.New("can't start clique chain without signers")
	}
	if err := g.Alloc.write(db, block.Hash()); err != nil {
		return nil, err
	}
	// 写入难度
	rawdb.WriteTd(db, block.Hash(), block.NumberU64(), block.Difficulty())
	// 写入区块
	rawdb.WriteBlock(db, block)

	rawdb.WriteReceipts(db, block.Hash(), block.NumberU64(), nil)
	rawdb.WriteCanonicalHash(db, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(db, block.Hash())
	rawdb.WriteHeadFastBlockHash(db, block.Hash())
	rawdb.WriteHeadHeaderHash(db, block.Hash())
	rawdb.WriteChainConfig(db, block.Hash(), config)
	return block, nil
}

// MustCommit writes the genesis block and state to db, panicking on error.
// The block is committed as the canonical head block.
func (g *Genesis) MustCommit(db ethdb.Database) *types.Block {
	block, err := g.Commit(db)
	if err != nil {
		panic(err)
	}
	return block
}

// GenesisBlockForTesting creates and writes a block in which addr has the given wei balance.
// 测试模式 创建并写入一个区块 指定一个地址有一定的余额
func GenesisBlockForTesting(db ethdb.Database, addr common.Address, balance *big.Int) *types.Block {
	g := Genesis{
		Alloc:   GenesisAlloc{addr: {Balance: balance}},
		BaseFee: big.NewInt(params.InitialBaseFee),
	}
	return g.MustCommit(db)
}

// DefaultGenesisBlock returns the Ethereum main net genesis block.
// 以太坊主网的默认创世快信息
func DefaultGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.MainnetChainConfig,
		Nonce:      66,
		ExtraData:  hexutil.MustDecode("0x11bbe8db4e347b4e8c937c1c8370e4b5ed33adb3db69cbdb7a38e1e50b1b82fa"),
		GasLimit:   5000,
		Difficulty: big.NewInt(17179869184),
		Alloc:      decodePrealloc(mainnetAllocData),
	}
}

// DefaultRopstenGenesisBlock returns the Ropsten network genesis block.
// 返回测试网Ropsten的创世快
func DefaultRopstenGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.RopstenChainConfig,
		Nonce:      66,
		ExtraData:  hexutil.MustDecode("0x3535353535353535353535353535353535353535353535353535353535353535"),
		GasLimit:   16777216,
		Difficulty: big.NewInt(1048576),
		Alloc:      decodePrealloc(ropstenAllocData),
	}
}

// DefaultRinkebyGenesisBlock returns the Rinkeby network genesis block.
// 返回测试网Rinkeby的创世快
func DefaultRinkebyGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.RinkebyChainConfig,
		Timestamp:  1492009146,
		ExtraData:  hexutil.MustDecode("0x52657370656374206d7920617574686f7269746168207e452e436172746d616e42eb768f2244c8811c63729a21a3569731535f067ffc57839b00206d1ad20c69a1981b489f772031b279182d99e65703f0076e4812653aab85fca0f00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
		GasLimit:   4700000,
		Difficulty: big.NewInt(1),
		Alloc:      decodePrealloc(rinkebyAllocData),
	}
}

// DefaultGoerliGenesisBlock returns the Görli network genesis block.
// 返回测试网Görli的创世快
func DefaultGoerliGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.GoerliChainConfig,
		Timestamp:  1548854791,
		ExtraData:  hexutil.MustDecode("0x22466c6578692069732061207468696e6722202d204166726900000000000000e0a2bd4258d2768837baa26a28fe71dc079f84c70000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
		GasLimit:   10485760,
		Difficulty: big.NewInt(1),
		Alloc:      decodePrealloc(goerliAllocData),
	}
}

// DefaultSepoliaGenesisBlock returns the Sepolia network genesis block.
// 返回测试网Sepolia的创世快
func DefaultSepoliaGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.SepoliaChainConfig,
		Nonce:      0,
		ExtraData:  []byte("Sepolia, Athens, Attica, Greece!"),
		GasLimit:   0x1c9c380,
		Difficulty: big.NewInt(0x20000),
		Timestamp:  1633267481,
		Alloc:      decodePrealloc(sepoliaAllocData),
	}
}

func DefaultKilnGenesisBlock() *Genesis {
	g := new(Genesis)
	reader := strings.NewReader(KilnAllocData)
	if err := json.NewDecoder(reader).Decode(g); err != nil {
		panic(err)
	}
	return g
}

// DeveloperGenesisBlock returns the 'geth --dev' genesis block.
// 返回 开发模式的创世区块
func DeveloperGenesisBlock(period uint64, gasLimit uint64, faucet common.Address) *Genesis {
	// Override the default period to the user requested one
	config := *params.AllCliqueProtocolChanges
	config.Clique = &params.CliqueConfig{
		Period: period,
		Epoch:  config.Clique.Epoch,
	}

	// Assemble and return the genesis with the precompiles and faucet pre-funded
	return &Genesis{
		Config:     &config,
		ExtraData:  append(append(make([]byte, 32), faucet[:]...), make([]byte, crypto.SignatureLength)...),
		GasLimit:   gasLimit,
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Difficulty: big.NewInt(1),
		Alloc: map[common.Address]GenesisAccount{
			common.BytesToAddress([]byte{1}): {Balance: big.NewInt(1)}, // ECRecover
			common.BytesToAddress([]byte{2}): {Balance: big.NewInt(1)}, // SHA256
			common.BytesToAddress([]byte{3}): {Balance: big.NewInt(1)}, // RIPEMD
			common.BytesToAddress([]byte{4}): {Balance: big.NewInt(1)}, // Identity
			common.BytesToAddress([]byte{5}): {Balance: big.NewInt(1)}, // ModExp
			common.BytesToAddress([]byte{6}): {Balance: big.NewInt(1)}, // ECAdd
			common.BytesToAddress([]byte{7}): {Balance: big.NewInt(1)}, // ECScalarMul
			common.BytesToAddress([]byte{8}): {Balance: big.NewInt(1)}, // ECPairing
			common.BytesToAddress([]byte{9}): {Balance: big.NewInt(1)}, // BLAKE2b
			faucet:                           {Balance: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(9))},
		},
	}
}

func decodePrealloc(data string) GenesisAlloc {
	var p []struct{ Addr, Balance *big.Int }
	if err := rlp.NewStream(strings.NewReader(data), 0).Decode(&p); err != nil {
		panic(err)
	}
	ga := make(GenesisAlloc, len(p))
	for _, account := range p {
		ga[common.BigToAddress(account.Addr)] = GenesisAccount{Balance: account.Balance}
	}
	return ga
}
