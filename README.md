# hitter

Load test for new Mongo cluster.

## Testing

    ginkgo watch -tags test --notify --randomizeAllSpecs  -skipMeasurements -gcflags=-l -r

Standalone test instance, to check out the web browser:

    go build -tags test && ./hitter

Then go to port 8088 in a web browser.

## Building for production

    go-bindata -pkg web -o web/assets.go assets assets/**/*(/)
    go build

## Deploying to production

From the source code:

    aws s3 cp hitter s3://lyfe-receiver/production/

From the Lyfe Ansible repo:

    source venv/bin/activate
    ansible-playbook --extra-vars "hosts=tag_Type_hitter" hitter_deploy.yaml

## Making the data

For prod data:

    go-bindata -pkg data -tags '!test' -o data/proddata.go logs
