package engineconfig

import (
	"strconv"

	"github.com/nspcc-dev/neofs-node/cmd/neofs-node/config"
	shardconfig "github.com/nspcc-dev/neofs-node/cmd/neofs-node/config/engine/shard"
)

const (
	subsection = "storage"

	// ShardPoolSizeDefault is a default value of routine pool size per-shard to
	// process object PUT operations in a storage engine.
	ShardPoolSizeDefault = 20
)

// IterateShards iterates over subsections of "shard" subsection of "storage" section of c,
// wrap them into shardconfig.Config and passes to f.
//
// Section names are expected to be consecutive integer numbers, starting from 0.
//
// Panics if N is not a positive number while shards are required.
func IterateShards(c *config.Config, required bool, f func(*shardconfig.Config)) {
	c = c.Sub(subsection)

	c = c.Sub("shard")
	def := c.Sub("default")

	i := uint64(0)
	for ; ; i++ {
		si := strconv.FormatUint(i, 10)

		sc := shardconfig.From(
			c.Sub(si),
		)

		// Path for the blobstor can't be present in the default section, because different shards
		// must have different paths, so if it is missing, the shard is not here.
		// At the same time checking for "blobstor" section doesn't work proper
		// with configuration via the environment.
		if (*config.Config)(sc).Value("blobstor.path") == nil {
			break
		}
		(*config.Config)(sc).SetDefault(def)

		f(sc)
	}
	if i == 0 && required {
		panic("no shard configured")
	}
}

// ShardPoolSize returns the value of "shard_pool_size" config parameter from "storage" section.
//
// Returns ShardPoolSizeDefault if the value is not a positive number.
func ShardPoolSize(c *config.Config) uint32 {
	v := config.Uint32Safe(c.Sub(subsection), "shard_pool_size")
	if v > 0 {
		return v
	}

	return ShardPoolSizeDefault
}

// ShardErrorThreshold returns the value of "shard_ro_error_threshold" config parameter from "storage" section.
//
// Returns 0 if the the value is missing.
func ShardErrorThreshold(c *config.Config) uint32 {
	return config.Uint32Safe(c.Sub(subsection), "shard_ro_error_threshold")
}
