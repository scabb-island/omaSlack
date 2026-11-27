## ADDED Requirements

### Requirement: Reload method

The daemon SHALL expose a `workspaces.reload` method that makes it read the
keyring again and bring its workspaces in line with it, the same effect as
the SIGHUP reload. It takes no parameters and returns once the reload has
been requested, so `omaslack auth add` and `omaslack auth refresh` take
effect without `systemctl --user reload omaslack`.

#### Scenario: A client asks the daemon to reload

- **WHEN** a client calls `workspaces.reload` after the keyring changed
- **THEN** the daemon reads the keyring again, starts added workspaces, stops removed ones and restarts those whose credentials changed
