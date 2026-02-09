# Key-Value Storage Library

An Elixir library that provides in-memory key-value storage using a GenServer.

## Behavior

- Store, retrieve, and delete values by string key
- Values can be any Erlang term (maps, lists, tuples, binaries, etc.)
- Each store is an isolated namespace (multiple stores can coexist)
- Data lives in memory only — lost when the owning process stops

## API

```elixir
# Start a named store
{:ok, pid} = KvStore.start_link(name: :my_store)

# Write
:ok = KvStore.put(:my_store, "user:1", %{name: "Alice", role: :admin})

# Read
{:ok, %{name: "Alice", role: :admin}} = KvStore.get(:my_store, "user:1")
:error = KvStore.get(:my_store, "nonexistent")

# Delete
:ok = KvStore.delete(:my_store, "user:1")

# List keys
["user:2", "user:3"] = KvStore.keys(:my_store)

# Check existence
true = KvStore.exists?(:my_store, "user:2")
```

## Acceptance Criteria

- `put/3` stores a key-value pair
- `get/2` returns `{:ok, value}` or `:error`
- `delete/2` removes a key; deleting a nonexistent key returns `:ok`
- `keys/1` returns all keys in the store as a list of strings
- `exists?/2` returns a boolean
- All operations are serialized through the GenServer (no concurrency issues)
- Library ships as a proper Mix project with tests

## Constraints

- No external dependencies — stdlib and OTP only
- GenServer state is a dedicated module struct (e.g. `KvStore.State`) — not a plain map
- Name registration uses `Registry` (not `:name` / global / `:via` with a custom module). Start a `KvStore.Registry` in the supervision tree; GenServer processes register through it via `{:via, Registry, {KvStore.Registry, name}}`
- All reads and writes go through the GenServer

## Out of Scope

- Persistence / disk backing
- TTL / expiration
- Distributed / multi-node replication
- Max size / eviction
- Key iteration ordering guarantees
