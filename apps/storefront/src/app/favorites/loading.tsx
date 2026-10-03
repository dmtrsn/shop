import { Container } from "@/components/ui/container";
import { FavoritesSkeleton } from "@/components/storefront/favorites-view";
import styles from "@/components/storefront/favorites.module.css";

export default function FavoritesLoading() {
  return <main className={styles.main} id="main-content"><Container>
    <header className={styles.heading}><h1>Избранное</h1></header>
    <FavoritesSkeleton />
  </Container></main>;
}
