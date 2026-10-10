# Prize medium catalogue

## The behavioural framing, and why it comes before the number

MLH's organizer guide opens the prize decision on behaviour rather than value: when selecting prizes, "it's important to consider why you are giving a prize, what sort of behaviors that prize rewards, and how you can go about motivating those behaviors." Prizes "say a lot about who you are as a community, why you are there in the first place, and they influence the vibe of your event - big time."

Its position on cash is stated directly: "While cash prizes are easy for events, we do not recommend them. The perceived value of a cash prize is much lower than traditional prizes."

Two things transfer: the direction of the claim, and the framing that a prize signals something about the event rather than paying for work. Evidentiary weight does not transfer.

This is one organization's stated position on its own event format. It is not a comparison of measured outcomes, and treating it as one overstates it in the opposite direction from the assumption it corrects.

## The five rungs, with published examples

**Gear** - hardware and tools the winner keeps and uses. MLH's own named category is "Gear - Cool Items To Use With Future Hacks", listing development kits, components, battery packs and gift cards to component suppliers. Its hardware-hackathon guide goes further, naming curated themed kits, high-value components too expensive for a student to buy individually, and professional tools: multimeters, oscilloscopes, soldering stations. The stated rationale is that "The most impactful prizes support the winning teams' continued interest in hardware development."

**Credits and vouchers** - redeemable value that is not currency. MLH's hardware guide names makerspace and fabrication-lab credit vouchers, and a membership granting continued access to equipment. Cloud and SaaS credits are the common sponsor-funded version. General tax-compliance practice treats a gift card or voucher as a cash equivalent for reporting purposes even though it never touches a payment rail, which is why this rung sits next to cash on the compliance axis rather than next to gear.

Every credit carries three failure points the organizer should publish:

- an expiry date
- a redemption region
- an account the winner must create

**Cash** - currency transferred to an individual. Cheapest to procure, and the only rung that necessarily moves through a payment rail. That rail is documented, not hypothetical: Devpost's own prize-claim process requires a cash winner to submit a tax-identification form before funds release, with a separate form for an international individual and another for an organization, and the platform states plainly that determining which one applies is each winner's own responsibility. That is the same claim-and-identity mechanic the payout-rigor menu calls the standard-form default.

**Opportunity** - an award whose value is access rather than a thing. MLH's own "Experiences" category is exactly this rung: conference tickets, "Lunch with your hero", a company visit and hang-out day. Its hardware guide adds the most ambitious version: where an incubator or venture funder is a partner, "offer the winning team an opportunity to present their project for future seed funding."

So the rung is documented practice. Its legal treatment is not, which is what keeps it the least confident cell on the compliance axis.

**Recognition only** - a trophy, a title, a public write-up, no transferable value. MLH lists custom trophies among its own prize ideas.

A civic-tech organizer states their own practice outright: "don't make winners and losers. Just don't. There has never been beer, competitions, or time pressure at my hackathons." That is a seasoned organizer describing events with no competition at all. The rung is therefore genuinely chosen, though it does not necessarily fit an event that has already published a judged rank.

## Procurement practice

- **Order early for stock risk, not shipping time.** "Many cool items will be backordered. Order prizes early to have them in time." That is availability risk on the vendor's side rather than a deadline on the organizer's, so it does not move with the rest of the event timeline. `samber/dev-event-organizer-skills@event-vendor-sourcing` owns the procurement itself.
- **Keep receipts, and plan for the leftover.** "Save the receipts for any prizes. If a winning team has 3 members instead of 4 you can return the prize to add to the next year's budget." The same source names two other uses for a leftover unit: raffle it, or attach it to a social-media challenge.
- **Do not price an in-kind prize at its retail value.** `samber/dev-event-organizer-skills@event-sponsor-pricing` values in-kind contributions at the budget line they displace rather than at the donor's stated retail price. That method assumes the displaced spend was one the organizer would otherwise have made, which is exactly what a prize may not be. Check the assumption before applying the method.

## Why a non-cash split has an arithmetic a cash split does not

A physical or credit prize is counted in units per person. A cash prize divides arbitrarily. MLH raises the consequence as one of the rules an organizer must decide before publishing: "Can the teams be as large as they want, and you might just have only 4 prizes?"

Three things follow from that constraint:

1. The team-size cap in the published rules sets the maximum unit count a single award consumes. That cap belongs to `samber/dev-event-organizer-skills@hackathon-brief-design`; you consume it.
2. A winning team smaller than the cap leaves units over. Decide their fate before the event (returned, raffled, or held) rather than at the podium.
3. A winning team larger than the cap cannot happen if the rule is enforced, but a team that added a member informally can. Publish whether the award follows the submitted team roster or the people standing on stage.

None of this applies to cash, which is the one genuine administrative advantage cash has and the reason it keeps getting chosen.
