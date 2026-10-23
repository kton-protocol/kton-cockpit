# Programme Respiratory — strategy

*Fictional example for the kton recorder. Names, decisions and studies are invented; the PK data
are R's `datasets::Theoph` and `datasets::Indometh`.*

**Indication:** persistent asthma and COPD in adults.

**Aim:** an oral maintenance treatment that can be dosed once or twice daily with predictable
exposure, so that therapeutic drug monitoring becomes the exception.

## Projects

| Project | Compound | Question for the programme |
|---|---|---|
| Theophylline | theophylline, modified release | Can exposure be predicted well enough from weight and dose to drop routine TDM? |
| Compound X | an investigational compound | Is its clearance low enough for once-daily dosing? |

## How decisions are made

Each project reports its pharmacokinetic model and its simulated exposure at the planned doses.
The programme compares them against the target product profile (`target-product-profile.md`)
before any phase II dose is chosen.
