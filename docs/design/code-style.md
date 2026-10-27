## Language and Runtime

To minimize both build and runtime requirements, use Golang for everything. Make a single binary that defines CLI entrypoints for all functions.

Always prefer the standard library, except when another solution is practically ubiquitous in the Golang community. Before including any third-party library, discuss the pros and cons of a domain-specific rewrite.
