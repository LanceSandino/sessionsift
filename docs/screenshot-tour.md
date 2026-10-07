# Screenshot tour

All screenshots in this tour were captured from synthetic demo sessions. They
contain no real transcript content, account credentials, local network details,
or embedded image metadata.

## 1. Read a conversation with its evidence

![Conversation lens with a selected tool call and redacted evidence](screenshots/01-workspace-conversation.webp)

The Conversation lens groups each prompt with its tool run. Failures, retries,
and later successes remain visible, while the inspector shows bounded, redacted
arguments and results for the selected item.

![Dark theme conversation workspace](screenshots/02-workspace-dark.webp)

The same workspace supports system, light, and dark themes without changing the
evidence model.

## 2. Follow the work chronologically

![Timeline lens with prompt checkpoints](screenshots/03-timeline.webp)

Timeline checkpoints keep failures and notable events near the prompt that
caused them. Long conversations load recent checkpoints first and can reveal
older evidence on demand.

![Tools lens grouped by observed tool](screenshots/04-tools.webp)

The Tools lens groups observed calls by tool and transport. It does not infer a
tool run that the imported source did not record.

## 3. Review without changing the source conversation

![Review lens with decisions and owner notes](screenshots/05-review.webp)

Notes and review decisions are local annotations. They are not sent back to the
source agent and do not modify the imported transcript.

## 4. Check capture and aggregate patterns

![Diagnostics page with capture and source status](screenshots/08-diagnostics.webp)

Diagnostics separates optional inputs from failures and shows whether each
configured source is currently producing records.

![Analytics page with privacy-safe aggregate totals](screenshots/09-analytics.webp)

Analytics uses aggregate metadata and deterministic findings. Prompt text,
assistant text, tool arguments, and tool results are excluded from analytics.

![Command palette with sessions and actions](screenshots/10-command-palette.webp)

The command palette opens with `Ctrl/Cmd K` for fast navigation between
sessions, lenses, and application pages.

## 5. Use the local workspace on a phone-sized screen

<table>
<tr>
<td><img src="screenshots/11-phone.webp" alt="Conversation on a phone-sized screen" width="280"><br><b>Conversation</b>: focused evidence at 390px.</td>
<td><img src="screenshots/12-phone-library.webp" alt="Session library drawer on a phone-sized screen" width="280"><br><b>Library</b>: pages and sessions in one drawer.</td>
</tr>
</table>

## 6. Optional remote support

The following flow is not needed for local SessionSift use. It requires the
separate hosted bridge and an identity-aware proxy configured by the operator.

![One-session support code dialog](screenshots/06-support-code.webp)

The source device creates a short-lived, single-session code and chooses a
view, comment, or review role.

![Connections page with local and remote access methods](screenshots/07-connections.webp)

Connections explains which access method is active and keeps device pairing,
email grants, and support sessions explicit.

![Helper's one-session bridge view](screenshots/13-helper-view.webp)

After the source device verifies the code, the helper receives only the granted
session and role—not a session list or generic workstation access.
