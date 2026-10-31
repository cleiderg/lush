# Visual style and client polish

**Part of:** [[PRD]] · **Status:** Milestone 8 (D-050)

> How Lush should look and feel. Mostly client work: Client render (lighting, day and night, terrain, props, characters, interiors), Client (HUD, panels, prompts, speech bubbles), with small Server world and Protocol pieces (the world clock, interior areas).

## The target: two reference screenshots

The owner's references are on the bot Mac in **`/Users/Shared/lush-art/reference/`**, outside the repo so they don't appear on GitHub. Every agent run can read that folder (the runner adds it with `--add-dir`). Open the images with the Read tool before starting any milestone 8 work. Never copy them into the repo. If the folder is missing, finish with `needs_owner` and say so.

| File | Shows |
|---|---|
| `night-cave.jpg` | Night or underground: dark scene lit by a campfire, glowing mushrooms and crystals, a pond, many small props (bones, pumpkins, chests, fence), speech bubble, location panel, round minimap, portraits |
| `day-tavern.jpg` | Late afternoon outdoors and a building interior: long warm sun shadows on grass, a cut-away building you can see into, wooden floor, fireplace, bar with bottles and barrels, candles, chandelier, tables with food, benches, lamp post, potted plants, the "Talk to …" prompt |

**Lush should reach the same level of detail, lighting, prop density and UI polish as these two images.** Every client-render and UI issue in this milestone names the reference it is matched against, and its PR is checked against it with screenshots (see Checking against the references).

The references are the owner's own mock-ups, not another game. **Copy them about 90%:** the same art style, model style and level of detail, the same lighting and colour mood, the same kinds and density of props, and the same UI styling (panels, borders, typography, minimap, bubbles, prompt). Use an open-licensed serif font that looks the same (for example Cinzel or Alegreya SC from Google Fonts). The 10% that changes:
- **Names and text:** use Lush's own place names, NPC names and wording, not "Harvest Eve", "The Gilded Goose", "Old Fennick", "Grumblethorn's Cave" and so on.
- **Characters and creatures:** Lush's own player characters and NPCs ([[gameplay]]) in the same style, not the orc, troll, wizard or other characters in the screenshots.
- **Setting:** a UCI campus, so campus buildings and props in the same style instead of fantasy ones where they don't fit (see Props).
- **Layout:** Lush's controls stay at the bottom of the screen (see Interface).

## What makes the references look the way they do

### Lighting (the biggest difference from Lush today)
- **A real sun** that casts soft shadows from buildings, trees, props and characters. In the day reference the shadows are long and warm because it's late afternoon.
- **Warm point lights** for fires, candles, lamps and lanterns, each with a soft pool of light on the ground and a gentle flicker on flames.
- **Glowing things:** mushrooms, crystals and windows emit their own light and have a soft glow (bloom).
- **Dark nights:** at night the ambient light drops a lot and the scene is lit mostly by local lights, as in the cave reference. Never pitch black: the player's surroundings stay readable.
- Tone mapping and a slight warm colour grade; a soft vignette at the edges.

### Day and night
- **The game follows the real time in Irvine.** When it's 7 pm in Irvine it's dusk in Lush, and night when it's night on campus. The time zone is America/Los_Angeles, so daylight saving is handled.
- The sun's position follows Irvine's real sunrise and sunset for the date (latitude 33.646, longitude −117.843), so winter evenings get dark earlier, as on campus. The sun's angle and colour, sky colour, ambient light and fog follow it through dawn, day, dusk and night.
- Lamps, lanterns, windows and campfires switch on at sunset and off at sunrise.
- The server sends its clock (UTC) in Welcome, and the client works out Irvine time and the sun from that, so a phone with a wrong clock or time zone still sees the same sky as everyone else. (Small Server world and Protocol change.)
- A **developer-only** time control (Dawn, Noon, Dusk, Night and pause, like the panel in the references) appears with `?debug` in the URL and overrides the real Irvine time, for testing and screenshots. Players never see it.

### Ground and nature
- Grass with scattered low-poly grass tufts and small flowers, drawn with instancing.
- Dirt paths, stone paths and paved plazas look different from grass, with soft edges.
- Low-poly rocks, bushes and trees with varied shapes and sizes, not one repeated model.
- Water (the pond in [[skills]] later, fountains now) with a calm surface, a darker edge and stones around it, as in the cave reference.

### Props: lots of small things
The references feel alive because every area has many small props. For Lush, campus props instead of fantasy ones:
- **Everywhere outside:** benches, lamp posts, bins, bike racks with bikes, signs, planters, picnic tables, umbrellas, scooters, fences, low walls.
- **Aldrich Park:** trees of several kinds, rocks, flower beds, a few picnic blankets, birds.
- **Near the shop and spawn:** the market stall with crates, sacks, fruit and a striped awning; lanterns; a notice board.
- **Food and fun:** food carts, coffee cups, plates, pizza boxes, string lights.
- At least **5 different props per 10 × 10 m of a "busy" area** (spawn, shop, plazas, building entrances), and some in every area.
- All props are original low-poly models within the model budgets ([[architecture]]), grouped and instanced so they don't cost a draw call each.

### Buildings and interiors
- Building walls get detail: windows with frames (lit from inside at night), doors, trim, roof edges, different materials for brick, concrete, wood and glass.
- **At least one enterable interior in this milestone: a café on the Student Center's ground floor**, furnished as richly as the tavern reference: counter, menu board, tables and chairs, plates and cups, plants, warm lights, a rug or two, a fireplace or a big window. It's a safe area.
- **Cut-away view:** when the player is inside a building, its roof and the walls between the camera and the player fade out so the room is visible, exactly as in the day reference. Outside, roofs are visible.
- The server needs the interior's walls for collision and knows when a player is inside (an area like the others in [[map]]). (Small Server world change.)

### Characters
- Chunky, blocky low-poly characters with a big head (about a third of the height), as in the references: readable from the high camera. This replaces the slim OSRS-like proportions (5.2).
- Faces with simple eyes, and hair, hats and clothes as separate pieces in flat colours, so equipment still shows ([[gameplay]]).
- Idle breathing, walking and running animations, a wave, and simple attack and hit animations. NPCs get the same treatment.
- A soft round shadow under every character and NPC, as well as the sun shadow.

### Camera
- Higher and a little further back than today, closer to the references: about 55° above the horizon by default, framing roughly 20 m across on a phone held upright. Drag to rotate stays. Pinch (or the mouse wheel) zooms between a close and a far limit.

## Interface

Dark translucent panels with a thin gold border, rounded corners, a soft shadow, and a serif small-caps typeface for titles, as in the references. One shared set of UI styles (colours, borders, type sizes, spacing) used by every panel, so everything matches.

**Layout:** the bottom of the screen stays as it is today (5.1): the joystick bottom left and the action buttons (Chat, Interact, Wave, Run) bottom right, restyled to match but in the same places. Everything new from the references goes in the **top part of the screen** only, so it never covers the controls or the player's thumbs.

| Element | In the references | For Lush |
|---|---|---|
| **Location panel** | Top left: time, a small sun arc, area type, place name | Top left: the place name ("Aldrich Park", "Student Center Café", "PvP zone" in red), the area type, the Irvine time with the sun or moon on an arc |
| **Minimap** | Top right: round, gold rim, north marker | Round minimap with a gold rim and an N marker: paths, buildings, the player, other players as dots, the shop and bank as icons, the PvP zone in red. Tap to open a bigger map |
| **Portraits** | Bottom left: party portraits with names | Top left, under the location panel: the player's own portrait with an HP ring and combat level, in the same round gold-rimmed style. Not at the bottom, which belongs to the joystick |
| **Speech bubbles** | Rounded, dark or light, italic serif, with a tail | Overhead chat uses the same bubble style, fading after the overhead time |
| **Interact prompt** | Bottom centre: key hint, "Talk to *Name*", role tag | Top centre, between the location panel and the minimap: what Interact will do now ("Attack Coyote · Lvl 9", "Open Campus Market", "Pick up Acorn", "Trade with a player"), the name in gold. On desktop it shows the key |
| **Panels** | (not shown) | Inventory, equipment, shop, trade, card table and chat log restyled to match, opening where they open today. Large tap targets stay (5.1) |
| **Controls** | (keyboard game) | Joystick and action buttons stay at the bottom, restyled in the gold-and-dark style |

Small touches that make it feel polished: hover and tap feedback on every button, short open and close animations on panels (150 to 200 ms), a soft click sound on buttons, and floating "+XP" and gold numbers.

## Performance

The device floor and frame rate don't change: **30 fps on an iPhone 13** (5.2, D-031). To keep it:
- **Quality presets: Low, Medium, High,** chosen automatically from the device and frame rate, with a setting to override. High is the reference look (shadows, bloom, all lights, dense grass). Medium is the default on phones: one shadow map at a lower resolution, half-resolution bloom, fewer grass tufts. Low turns off shadows and bloom but keeps the lighting colours and props.
- At most **8 point lights** active at once, the ones nearest the camera; others fade out smoothly or fall back to a glowing material without light.
- Instancing for grass, flowers and repeated props. Placeholders to tune after profiling: under 150 draw calls and 300,000 visible triangles on Medium.
- The retro toggle (5.2) stays and works on top of the new look.

## Checking against the references

- A script `npm run shots` in `client/` starts the client with a fixed seed and no network (bots stand in for players), and saves screenshots to `client/.shots/` (not committed): spawn at noon, spawn at night, the park at dusk, the café interior, and each UI panel open. Phone (portrait, 390 × 844) and desktop (1600 × 1000).
- Every client-render or UI PR in this milestone runs it. The reviewer looks at the shots next to the matching reference and checks the issue's criteria ("night scene lit mostly by local lights", "café has at least 25 props", and so on) before approving.
- Acceptance criteria are written so a test can prove them where possible (light counts, prop counts per area, draw calls, presets switching), and the screenshots cover what tests can't.

## Out of scope for milestone 8

- Weather (rain, fog banks), seasons.
- New gameplay, NPCs or items (later milestones).
- Interiors other than the Student Center café.
- Server changes beyond the world clock and the café's interior area.
