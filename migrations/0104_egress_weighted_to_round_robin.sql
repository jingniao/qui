-- Phase 6.2: retire the weighted egress-group strategy (plan D4).
--
-- The removed fork cores could share connections by weight. Neither upstream
-- Xray (balancer strategies: random, roundRobin, leastPing, leastLoad) nor
-- upstream sing-box (selector, urltest) has a weighted selector, so the strategy
-- is removed from the API, the validation and the UI.
--
-- Existing groups are remapped to round_robin, the closest upstream behaviour
-- (Xray renders routing.balancers with strategy roundRobin; sing-box renders the
-- latency-tested urltest group). The CHECK constraint of 0010 already allows
-- round_robin, so no table is rebuilt.
--
-- The outbound_group_members.weight column is left in place: it stays part of
-- the stored member rows and is only remapped here, never read by a weighted
-- strategy again.

UPDATE outbound_groups SET strategy = 'round_robin' WHERE strategy = 'weighted';
