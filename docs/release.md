# Release

Release automation is blocked on
[T380127](https://phabricator.wikimedia.org/T380127) and
[T194332](https://phabricator.wikimedia.org/T194332). Meanwhile,
we have a manual release process, which is not great.

```sh
TAG=0.0.7

git clone git@github.com:brawer/osmviews.git
cd osmviews
go test ./...
git tag $TAG
git push origin $TAG
GOOS=linux GOARCH=amd64 go build ./cmd/osmviews-builder
GOOS=linux GOARCH=amd64 go build ./cmd/webserver
ssh login.toolforge.org mkdir /data/project/osmviews/bin/$TAG
scp -r osmviews-builder login.toolforge.org:/data/project/osmviews/bin/$TAG/osmviews-builder
scp -r webserver login.toolforge.org:/data/project/osmviews/bin/$TAG/webserver

# Make sure to replace 0.0.7 with current $TAG below
ssh login.toolforge.org
become osmviews
toolforge jobs flush
toolforge jobs run osmviews-builder --command bin/0.0.7/osmviews-builder --image bookworm --schedule "@daily"
toolforge webservice bookworm stop
toolforge webservice bookworm start bin/0.0.7/webserver
```

