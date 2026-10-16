import { FavoritesSkeleton } from "@/components/storefront/favorites-view";
import { Container } from "@/components/ui/container";
import { PageHeader } from "@/components/ui/page-header";
import styles from "@/components/storefront/favorites.module.css";

export default function FavoritesLoading() {
  return <main className={styles.main} id="main-content"><Container>
    <PageHeader title="Избранное" />
    <FavoritesSkeleton />
  </Container></main>;
}
