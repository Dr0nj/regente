# Agent authentication and upgrades

Every agent transport requires a token created in **Settings → Agents → Create token**.
This applies to WebSocket, HTTP long-poll and SSE, including result and output requests.
The server API token and browser login sessions do not authenticate agents.

## New installations

1. Sign in as an administrator and open Settings → Agents.
2. Create a separate token for each agent and save the value shown once.
3. Configure the agent service with that value. Existing service installers store it
   in the agent's protected environment file; they do not need the server API token.
4. Start the agent and verify that it appears online. Execute a harmless test job
   to verify dispatch, output and completion.

For foreground use, pass the issued token with the agent's -token flag. The agent
also accepts it through REGENTE_TOKEN in its own process environment. An empty
credential stops startup with instructions. The environment variable name does not
make the server's administrative token valid for agents.

## Existing installations

Tokens already issued through Settings → Agents remain valid; this change does not
alter the database schema or require reissuing those tokens.

If an agent uses the server API token, a login session or dev-token:

1. Create a dedicated agent token before upgrading the server.
2. Update the agent's service configuration and restart the agent. Dedicated tokens
   also work with the previous server version, so this can be done first.
3. Verify a harmless job completes, then upgrade and restart the server.
4. Verify agents reconnect and repeat the job check. A 401 on an agent endpoint
   means the supplied credential was not accepted; check the agent configuration
   and whether its token was revoked.

Keep the administrative token in administrative tools that need it. Do not copy it
into an agent service to work around an authentication failure.

An application downgrade restores the previous authentication behavior; it is not
a security-preserving rollback. If an agent cannot reconnect, correct its dedicated
credential rather than re-enabling the shared-token path.
