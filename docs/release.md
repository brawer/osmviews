# Release

Release automation is blocked on
[T380127](https://phabricator.wikimedia.org/T380127) and
[T194332](https://phabricator.wikimedia.org/T194332). Meanwhile,
we have a manual release process, which is not great.

```sh
ssh login.toolforge.org
become osmviews
toolforge build start --use-latest-versions https://github.com/brawer/osmviews.git
toolforge webservice --mount=none buildservice restart
toolforge jobs flush
toolforge jobs run --image tool-osmviews/tool-osmviews:latest --mem 3G --cpu 2 --mount none --schedule @daily --command osmviews-builder osmviews-builder
```
