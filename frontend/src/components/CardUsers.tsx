import { useI18n } from "../i18n";

/** How many Agent chips a card shows before the rest collapse into "+N".
 *
 *  Three matches requiredByHint in internal/app/runtime.go, which renders
 *  "A, B, C +2" for the same kind of list. One convention for "too many to
 *  name" is worth more than a per-page choice.
 *
 *  The cap exists for layout, not brevity: .card-users wraps, so an uncapped
 *  list grew the card by a row of chips at a time. With eight configurable
 *  Agents that reached +36px, which is what made a bound card tower over an
 *  unbound one in the same grid row. */
const VISIBLE_CHIPS = 3;

export interface CardUser {
  id: string;
  name: string;
}

/** The Agent chips on a Provider or Profile card.
 *
 *  Shared rather than duplicated per page: both cards sit in grids that use the
 *  same rule (.provider-list / .profile-list), so a change to how many chips a
 *  card shows has to reach both or the two pages drift apart. */
export function CardUsers({ users }: { users: readonly CardUser[] }) {
  const { locale, t } = useI18n();
  if (!users.length) {
    return <span className="card-users is-empty">{t("暂无 Agent 使用")}</span>;
  }
  const shown = users.slice(0, VISIBLE_CHIPS);
  const hidden = users.slice(VISIBLE_CHIPS);
  return (
    <span className="card-users">
      {shown.map((user) => (
        <span className="card-user-chip" key={user.id}>{user.name}</span>
      ))}
      {hidden.length ? (
        // title, not a tooltip component: the names are the only thing the
        // collapsed chip hides, and they are worth recovering on hover without
        // adding an interactive control to a card that is already dense.
        <span className="card-user-chip is-overflow" title={hidden.map((user) => user.name).join(locale === "en" ? ", " : "、")}>
          +{hidden.length}
        </span>
      ) : null}
    </span>
  );
}
