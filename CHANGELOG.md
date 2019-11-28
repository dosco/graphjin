<a name="unreleased"></a>
## [Unreleased]


<a name="v0.12.4"></a>
## [v0.12.4] - 2019-11-28
### Move
- Move license from MIT to Apache 2.0. Add Makefile


<a name="v0.12.3"></a>
## [v0.12.3] - 2019-11-26
### Added
- Added support for query names to the allow.list


<a name="v0.12.2"></a>
## [v0.12.2] - 2019-11-25
### Fix
- Fix bug with compiling anon queries


<a name="v0.12.1"></a>
## [v0.12.1] - 2019-11-22
### Move
- Move sql query logging from info to debug


<a name="v0.12.0"></a>
## [v0.12.0] - 2019-11-22
### Use
- Use logger error instead of panic in goja handlers


<a name="v0.11.9"></a>
## [v0.11.9] - 2019-11-22
### Add
- Add a db:reset command only for dev mode


<a name="v0.11.8"></a>
## [v0.11.8] - 2019-11-21
### Optimize
- Optimize db queries limit use of transactions


<a name="v0.11.7"></a>
## [v0.11.7] - 2019-11-19
### Added
- Added support for multi-root queries


<a name="v0.11.6"></a>
## [v0.11.6] - 2019-11-15
### Fix
- Fix issues with JWT auth
- Fix bug with migration filename generation
- Fix bug with migration file name


<a name="v0.11.5"></a>
## [v0.11.5] - 2019-11-10
### Fix
- Fix bug with migration template name


<a name="v0.11.4"></a>
## [v0.11.4] - 2019-11-10
### Fix
- Fix bug with creating new migrations


<a name="v0.11.3"></a>
## [v0.11.3] - 2019-11-09
### Fix
- Fix macro syntax bug in app templates


<a name="v0.11.2"></a>
## [v0.11.2] - 2019-11-07
### Fix
- Fix bugs and add new production mode


<a name="v0.11.1"></a>
## [v0.11.1] - 2019-11-05
### Add
- Add nested where clause to filter based on related tables

### Block
- Block unauthorized requests when 'anon' role is not defined

### Update
- Update docs and website with new features


<a name="v0.11"></a>
## [v0.11] - 2019-11-01
### Add
- Add config driven presets for insert, update and upsert
- Add config driven presets for insert, update and upserta
- Add RBAC option to disable functions eg. count
- Add fuzz testing to 'serv' for the GQL hash parser
- Add fuzz testing to 'jsn' and 'qcode'
- Add ability to block queries and mutations by role
- Add built in 'anon' and 'user' roles
- Add role based access control

### Allow
- Allow config files to inherit from other config files

### Change
- Change config key inherit to inherits

### Get
- Get RBAC working for queries and mutations

### Optimize
- Optimize prepared statement flow for RBAC

### Preserve
- Preserve allow.list ordering on save

### Update
- Update filters section in guide

### Pull Requests
- Merge pull request [#11](https://github.com/dosco/super-graph/issues/11) from dosco/rbac


<a name="v0.10.1"></a>
## [v0.10.1] - 2019-10-06
### Add
- Add ability to set filters per operation / action
- Add upsert mutation

### Pull Requests
- Merge pull request [#10](https://github.com/dosco/super-graph/issues/10) from FourSigma/sm-examples-folder


<a name="v0.10"></a>
## [v0.10] - 2019-10-04
### Fix
- Fix return values for bulk mutations and delete
- Fix issues with mutation SQL
- Fix broken demo app
- Fix typo in 'across'

### Remove
- Remove extra link from README

### Update
- Update docs, getting started guide and mutations

### Pull Requests
- Merge pull request [#6](https://github.com/dosco/super-graph/issues/6) from muesli/typo-fixes


<a name="v0.9"></a>
## [v0.9] - 2019-10-01
### Fix
- Fix demo rails app broken build


<a name="v0.8"></a>
## [v0.8] - 2019-09-30
### Fix
- Fix invalid import bug

### Update
- Update documentation site


<a name="v0.7"></a>
## [v0.7] - 2019-09-29
### Failure
- Failure to prepare statements should be a warning

### Fix
- Fix duplicte column bug


<a name="v0.6"></a>
## [v0.6] - 2019-09-29
### Add
- Add database setup commands
- Add binary compression back to Dockerfile
- Add initialization command to setup new apps
- Add migrate command
- Add database seeding capability
- Add session variable for user id
- Add delete mutation
- Add update mutation
- Add insert mutation with bulk insert
- Add GoTO Aug, 19 presentation
- Add support for prepared statements
- Add end-to-end benchmaking
- Add object pooling for parser expressions
- Add request / response debugging for remote joins
- Add a presentation about GraphQL
- Add validation for remote JSON
- Add tracing for API stitching
- Add REST API stitching
- Add SQL query cacheing
- Add support for GraphQL variables
- Add fuzz testing to qcode
- Add test for Rails Redis cookie store integration
- Add an install guide

### Change
- Change fuzz test name to qcode
- Change logo from PNG to SVG

### Enabke
- Enabke reload on config change

### Fix
- Fix missing config name bug
- Fix new app templates
- Fix help message for migrate
- Fix session variable bug
- Fix test failures in `psql` and `serv`
- Fix demo docker services startup order
- Fix wrong value for false token bug. Reported by [@ThisIsMissEm](https://github.com/ThisIsMissEm)
- Fix allow.list file discovery bug
- Fix bug with allow list path
- Fix wrong value for use_allow_list in dev config
- Fix startup bug in demo script
- Fix url bug in allow list
- Fix bug [#676](https://github.com/dosco/super-graph/issues/676) found by fuzzer
- Fix race-condition in remote joins
- Fix cookie passing in web ui
- Fix bug with passing cookies in web ui
- Fix null pointer with invalid argument values
- Fix infinite loop bug in lexer
- Fix null pointer issue found by fuzz test
- Fix issue with fuzzbuzz config
- Fix demo to run as memory only
- Fix auth documentation
- Fix issue with web ui sizing
- Fix issue preventing docker-compose deploy
- Fix try demo documentation

### Futher
- Futher reduce allocations across hot paths
- Futher reduce allocations on the compiler hot path
- Futher optimize json parsing and editing performance

### Highlight
- Highlight top features better on the site

### Improve
- Improve readability of json parser code
- Improve the motivation section in the readme
- Improve the demo experience

### Make
- Make remote joins use parallel http requests

### Merge
- Merge branch 'master' into optimize-psql

### New
- New low allocation fast json parsing and editing library

### Optimize
- Optimize lexer and fix bugs
- Optimize the sql generator hot path

### Reduce
- Reduce alllocations done by the stack
- Reduce steps to run the demo
- Reduce allocations and improve perf over 50%

### Remove
- Remove unused packages
- Remove the 'hello' test app folder
- Remove other allocations in psql

### Use
- Use hash's as ids for table relationships

### Watch
- Watch and reload on config changes


<a name="v0.5"></a>
## [v0.5] - 2019-04-10
### Add
- Add supprt for new Rails 5.2 aes-256-gcm cookies
- Add query support for ts_rank and ts_headline
- Add full text search support using TSV indexes
- Add missing assets folder
- Add fetch by ID feature
- Add documentation

### Cleanup
- Cleanup and redesign config files

### Fix
- Fix bug with auth config parsing

### Redesign
- Redesign config file architecture

### Reduce
- Reduce realloc of maps and slices

### Update
- Update docs with full-text search information


<a name="v0.4"></a>
## [v0.4] - 2019-04-01

<a name="v0.3"></a>
## [v0.3] - 2019-04-01
### Add
- Add SQL execution timing and tracing
- Add support for HAVING with aggregate queries
- Add aggregrate functions to GQL queries
- Add Auth0 JWT support
- Add React UI building to the docker build flow
- Add compiler profiling
- Add bechmarks for GQL to SQL compile
- Add tests for gql to sql compile

### Cleanup
- Cleanup Dockerfile

### Fix
- Fix recurring packer issue docker hub builds
- Fix issue with asset packer breaking Docker builds
- Fix missing git package in Dockerfile
- Fix docker ignore values
- Fix image build failure on docker hub
- Fix build issue in Dockerfile
- Fix bugs and document the 'where' clause
- Fix perf issue with inflections

### Optimize
- Optimize docker image

### Pack
- Pack web UI with app into a single binary

### Upgrade
- Upgrade web UI packages


<a name="0.3"></a>
## 0.3 - 2019-03-24
### First
- First commit

### Fix
- Fix license to MIT


[Unreleased]: https://github.com/dosco/super-graph/compare/v0.12.4...HEAD
[v0.12.4]: https://github.com/dosco/super-graph/compare/v0.12.3...v0.12.4
[v0.12.3]: https://github.com/dosco/super-graph/compare/v0.12.2...v0.12.3
[v0.12.2]: https://github.com/dosco/super-graph/compare/v0.12.1...v0.12.2
[v0.12.1]: https://github.com/dosco/super-graph/compare/v0.12.0...v0.12.1
[v0.12.0]: https://github.com/dosco/super-graph/compare/v0.11.9...v0.12.0
[v0.11.9]: https://github.com/dosco/super-graph/compare/v0.11.8...v0.11.9
[v0.11.8]: https://github.com/dosco/super-graph/compare/v0.11.7...v0.11.8
[v0.11.7]: https://github.com/dosco/super-graph/compare/v0.11.6...v0.11.7
[v0.11.6]: https://github.com/dosco/super-graph/compare/v0.11.5...v0.11.6
[v0.11.5]: https://github.com/dosco/super-graph/compare/v0.11.4...v0.11.5
[v0.11.4]: https://github.com/dosco/super-graph/compare/v0.11.3...v0.11.4
[v0.11.3]: https://github.com/dosco/super-graph/compare/v0.11.2...v0.11.3
[v0.11.2]: https://github.com/dosco/super-graph/compare/v0.11.1...v0.11.2
[v0.11.1]: https://github.com/dosco/super-graph/compare/v0.11...v0.11.1
[v0.11]: https://github.com/dosco/super-graph/compare/v0.10.1...v0.11
[v0.10.1]: https://github.com/dosco/super-graph/compare/v0.10...v0.10.1
[v0.10]: https://github.com/dosco/super-graph/compare/v0.9...v0.10
[v0.9]: https://github.com/dosco/super-graph/compare/v0.8...v0.9
[v0.8]: https://github.com/dosco/super-graph/compare/v0.7...v0.8
[v0.7]: https://github.com/dosco/super-graph/compare/v0.6...v0.7
[v0.6]: https://github.com/dosco/super-graph/compare/v0.5...v0.6
[v0.5]: https://github.com/dosco/super-graph/compare/v0.4...v0.5
[v0.4]: https://github.com/dosco/super-graph/compare/v0.3...v0.4
[v0.3]: https://github.com/dosco/super-graph/compare/0.3...v0.3
