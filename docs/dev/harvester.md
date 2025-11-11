# gnasty-harvester image guidance

The `gnasty-harvester` service in this repository always pulls the published
[`gnasty-chat`](https://github.com/hpwn/gnasty-chat) container image declared by
`GNASTY_IMAGE`. The Docker Compose file defaults this to `gnasty-chat:latest`, so
any edits to the harvester must be made in the upstream `gnasty-chat` repository
(and its image build pipeline) rather than here.

## Confirming the resolved image tag

Run `docker compose config` to inspect the fully rendered configuration and
capture the section for `gnasty-harvester`. This command resolves the tag to the
exact image (for example `gnasty-chat:latest`) that Docker Compose will pull:

```bash
docker compose config | sed -n '/gnasty-harvester:/,/image:/p'
```

Save the relevant snippet alongside your change request or support ticket so
reviewers can see which image tag is in use.
